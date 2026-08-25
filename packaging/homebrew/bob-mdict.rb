# typed: strict
# frozen_string_literal: true

# Homebrew formula for bob-mdict.
#
# Ship this in a tap (wakewon/homebrew-tap) so users can install with:
#
#   brew install wakewon/tap/bob-mdict
#   brew services start bob-mdict
#
# Replace the placeholder sha256 values with the ones in release/SHA256SUMS
# after running scripts/build-server.sh for a tagged release.
class BobMdict < Formula
  desc "Local MDict dictionary service for the Bob MDict plugin"
  homepage "https://github.com/wakewon/bob-plugin-mdict"
  version "0.2.1"
  license "GPL-3.0-or-later"

  # A handful of older dictionaries store pronunciations as Ogg-Speex, which
  # macOS cannot play. speexdec transcodes them once and the result is cached.
  # This project never synthesizes speech, so without the decoder those
  # particular pronunciations are simply not offered.
  depends_on "speex"

  on_macos do
    on_arm do
      url "https://github.com/wakewon/bob-plugin-mdict/releases/download/v#{version}/bob-mdict-#{version}-darwin-arm64.tar.gz"
      sha256 "REPLACE_WITH_ARM64_SHA256"
    end
    on_intel do
      url "https://github.com/wakewon/bob-plugin-mdict/releases/download/v#{version}/bob-mdict-#{version}-darwin-amd64.tar.gz"
      sha256 "REPLACE_WITH_AMD64_SHA256"
    end
  end

  def install
    bin.install "bob-mdict"
    (var/"log").mkpath
  end

  service do
    run [opt_bin/"bob-mdict"]
    keep_alive successful_exit: false
    log_path var/"log/bob-mdict.log"
    error_log_path var/"log/bob-mdict.log"
    # speexdec lives in the Homebrew prefix, which is not on launchd's default PATH.
    environment_variables PATH: std_service_path_env
  end

  def caveats
    <<~EOS
      bob-mdict reads dictionaries you supply. It does not ship any dictionary data.

      Put each dictionary in its own folder here:

        ~/Library/Application Support/bob-mdict/dictionaries/

      For example:

        dictionaries/My Dictionary/My Dictionary.mdx
        dictionaries/My Dictionary/My Dictionary.mdd
        dictionaries/My Dictionary/My Dictionary.1.mdd

      Then start the service and check it:

        brew services start bob-mdict
        bob-mdict --check

      Finally install the Bob plugin from:
        https://github.com/wakewon/bob-plugin-mdict/releases
    EOS
  end

  test do
    assert_match "bob-mdict", shell_output("#{bin}/bob-mdict --version")
    assert_match "api=v2", shell_output("#{bin}/bob-mdict --version")
  end
end
