{
  description = "Dependabot CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };

        dependabot-cli = pkgs.buildGoModule {
          pname = "dependabot-cli";
          version = "0.0.0-nix";

          src = pkgs.lib.cleanSource ./.;

          # After the first build, replace this hash with the actual one
          # reported by Nix (or use `nix build 2>&1` to find it).
          vendorHash = "sha256-K9pfvUeytod+sA33QOcN8BCuyRL0qHMdS5z/TKq3exM=";

          subPackages = [ "cmd/dependabot" ];

          # Tests are integration tests requiring Docker — skip in Nix sandbox.
          doCheck = false;

          ldflags = [
            "-s"
            "-w"
            "-X github.com/dependabot/cli/cmd/dependabot/internal/cmd.version=0.0.0-nix"
          ];

          env.CGO_ENABLED = 0;

          meta = with pkgs.lib; {
            description = "Dependabot CLI";
            homepage = "https://github.com/dependabot/cli";
            license = licenses.mit;
          };
        };

        # Minimal Docker image containing only the CLI binary, CA certs, and
        # git (needed for local-dir operations).
        #
        #   docker load < $(nix build .#dockerImage)
        #   docker run -v /var/run/docker.sock:/var/run/docker.sock \
        #     dependabot-cli update go_modules owner/repo
        #
        dockerImage = pkgs.dockerTools.buildImage {
          name = "dependabot-cli";
          tag = "latest";

          copyToRoot = pkgs.buildEnv {
            name = "image-root";
            paths = [
              dependabot-cli
              pkgs.cacert # TLS CA certificates
              pkgs.gitMinimal # git for local-dir operations
              pkgs.dockerTools.caCertificates
            ];
            pathsToLink = [
              "/bin"
              "/etc"
            ];
          };

          config = {
            Entrypoint = [ "${dependabot-cli}/bin/dependabot" ];
            Cmd = [ "--help" ];
            Env = [
              "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt"
            ];
          };
        };
      in
      {
        packages = {
          default = dependabot-cli;
          inherit dependabot-cli dockerImage;
        };

        devShells.default = pkgs.mkShell {
          inputsFrom = [ dependabot-cli ];
          buildInputs = with pkgs; [
            go
            gopls
            gotools
          ];
        };
      }
    );
}
