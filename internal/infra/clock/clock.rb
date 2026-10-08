# frozen_string_literal: true

# Leave monotonic clocks and OpenSSL's certificate clock untouched.
recorded_time = Rational(Integer(ENV.fetch("DEPENDABOT_RECORDED_AT"), 10), 1000)
Time.define_singleton_method(:now) do |**options|
  at(recorded_time, **options)
end
