package infra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type imageSelection struct {
	reference name.Digest
	localID   string
}

func verifyImages(ctx context.Context, cli *client.Client, params *RunParams, newVerifier func(context.Context) (*imageVerifier, error)) error {
	images := []struct {
		role      imageRole
		target    *string
		selection imageSelection
		verified  verifiedImage
	}{
		{role: proxyImageRole, target: &params.ProxyImage},
		{role: updaterImageRole, target: &params.UpdaterImage},
	}
	for i := range images {
		selected, err := selectImage(ctx, cli, *images[i].target, params.PullImages)
		if err != nil {
			return fmt.Errorf("%s image %s: failed to select image for verification: %w", images[i].role.name, *images[i].target, err)
		}
		images[i].selection = selected
	}
	verifier, err := newVerifier(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize updater/proxy image verification: %w", err)
	}
	for i := range images {
		verified, err := verifier.Verify(ctx, images[i].selection.reference, images[i].role)
		if err != nil {
			return err
		}
		images[i].verified = verified
	}
	if !params.PullImages {
		for _, selected := range images {
			inspect, err := inspectImageDigest(ctx, cli, selected.verified.reference)
			if err != nil {
				return fmt.Errorf("%s image %s: failed to inspect verified cached image: %w", selected.role.name, selected.verified.reference, err)
			}
			if inspect.ID != selected.selection.localID {
				return fmt.Errorf("%s image %s: verified digest does not match the selected cached image", selected.role.name, selected.verified.reference)
			}
		}
	}
	for _, selected := range images {
		*selected.target = selected.verified.reference.Name()
		log.Printf("verified %s image %s (producer commit %s)\n", selected.role.name, selected.verified.reference, selected.verified.producerCommit)
	}
	return nil
}

func selectImage(ctx context.Context, cli *client.Client, imageName string, pull bool) (imageSelection, error) {
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return imageSelection{}, err
	}
	if pull {
		options, err := imageRemoteOptions(ctx, ref)
		if err != nil {
			return imageSelection{}, err
		}
		descriptor, err := remote.Get(ref, options...)
		if err != nil {
			return imageSelection{}, fmt.Errorf("failed to resolve registry digest: %w", err)
		}
		if pinned, ok := ref.(name.Digest); ok && pinned.DigestStr() != descriptor.Digest.String() {
			return imageSelection{}, errors.New("registry manifest does not match the requested digest")
		}
		return imageSelection{reference: ref.Context().Digest(descriptor.Digest.String())}, nil
	}
	inspect, err := cli.ImageInspect(ctx, imageName)
	if err != nil {
		return imageSelection{}, err
	}
	if inspect.ID == "" {
		return imageSelection{}, errors.New("cached image has no image ID")
	}
	digest, err := cachedImageReference(ref, inspect.RepoDigests)
	if err != nil {
		return imageSelection{}, err
	}
	return imageSelection{reference: digest, localID: inspect.ID}, nil
}

func cachedImageReference(ref name.Reference, repoDigests []string) (name.Digest, error) {
	matches := make(map[string]struct{})
	var matched name.Digest
	for _, repoDigest := range repoDigests {
		digest, err := name.NewDigest(repoDigest)
		if err != nil {
			return name.Digest{}, fmt.Errorf("invalid cached repository digest: %w", err)
		}
		if digest.Context().Name() != ref.Context().Name() {
			continue
		}
		if pinned, ok := ref.(name.Digest); ok && pinned.DigestStr() != digest.DigestStr() {
			continue
		}
		matches[digest.Name()] = struct{}{}
		matched = digest
	}
	if len(matches) == 0 {
		return name.Digest{}, errors.New("no matching repository digest for cached image. Unsigned development images require --skip-image-verification")
	}
	if len(matches) > 1 {
		return name.Digest{}, errors.New("multiple repository digests for cached image. Use an immutable image reference")
	}
	return matched, nil
}

func inspectImageDigest(ctx context.Context, cli *client.Client, ref name.Digest) (image.InspectResponse, error) {
	inspect, err := cli.ImageInspect(ctx, ref.Name())
	if err != nil {
		return inspect, err
	}
	if inspect.ID == "" {
		return inspect, errors.New("cached image has no image ID")
	}
	if _, err := cachedImageReference(ref, inspect.RepoDigests); err != nil {
		return inspect, err
	}
	return inspect, nil
}

func pullVerifiedImage(ctx context.Context, cli *client.Client, imageName string) error {
	ref, err := name.NewDigest(imageName)
	if err != nil {
		return fmt.Errorf("verified image requires an immutable reference: %w", err)
	}
	if _, err := inspectImageDigest(ctx, cli, ref); err == nil {
		return nil
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("failed to inspect verified image %s: %w", ref, err)
	}
	auth, err := imageRegistryAuth(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to load registry credentials: %w", err)
	}
	config, err := authn.Authorization(ctx, auth)
	if err != nil {
		return fmt.Errorf("failed to authorize verified image pull: %w", err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to encode registry credentials: %w", err)
	}
	log.Printf("pulling verified image: %s\n", ref)
	stream, err := cli.ImagePull(ctx, ref.Name(), image.PullOptions{
		RegistryAuth: base64.URLEncoding.EncodeToString(encoded),
	})
	if err != nil {
		return fmt.Errorf("failed to pull verified image %s: %w", ref, err)
	}
	if err := readImagePull(stream); err != nil {
		return fmt.Errorf("failed to pull verified image %s: %w", ref, err)
	}
	if _, err := inspectImageDigest(ctx, cli, ref); err != nil {
		return fmt.Errorf("failed to inspect verified image %s after pull: %w", ref, err)
	}
	return nil
}

func readImagePull(stream io.ReadCloser) error {
	defer stream.Close()
	decoder := json.NewDecoder(stream)
	for {
		var message struct {
			Error       string `json:"error"`
			ErrorDetail *struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := decoder.Decode(&message); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		if message.Error != "" {
			return errors.New(message.Error)
		}
		if message.ErrorDetail != nil {
			return fmt.Errorf("Docker pull failed: %s", message.ErrorDetail.Message)
		}
	}
}
