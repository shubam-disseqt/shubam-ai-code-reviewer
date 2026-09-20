# typed: false
# frozen_string_literal: true

# sacr — deterministic, LLM-free code review CLI.
#
# This formula lives in the tap repo `shubam-disseqt/homebrew-tap` under
# `Formula/sacr.rb`. The release workflow in `shubam-disseqt/shubam-ai-code-reviewer`
# opens a PR that replaces the RELEASE_* placeholders below on every tagged
# release.
class Sacr < Formula
  desc "Deterministic, LLM-free code review CLI"
  homepage "https://github.com/shubam-disseqt/shubam-ai-code-reviewer"
  version "RELEASE_VERSION"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/shubam-disseqt/shubam-ai-code-reviewer/releases/download/v#{version}/sacr_#{version}_darwin_arm64.tar.gz"
      sha256 "RELEASE_SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "https://github.com/shubam-disseqt/shubam-ai-code-reviewer/releases/download/v#{version}/sacr_#{version}_darwin_amd64.tar.gz"
      sha256 "RELEASE_SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/shubam-disseqt/shubam-ai-code-reviewer/releases/download/v#{version}/sacr_#{version}_linux_arm64.tar.gz"
      sha256 "RELEASE_SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/shubam-disseqt/shubam-ai-code-reviewer/releases/download/v#{version}/sacr_#{version}_linux_amd64.tar.gz"
      sha256 "RELEASE_SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install "sacr"
  end

  test do
    output = shell_output("#{bin}/sacr version")
    assert_match "sacr", output
    assert_match version.to_s, output
  end
end
