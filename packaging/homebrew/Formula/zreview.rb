# typed: false
# frozen_string_literal: true

# zreview — deterministic, LLM-free code review CLI.
#
# This formula lives in the tap repo `shubam-disseqt/homebrew-tap` under
# `Formula/zreview.rb`. The release workflow in `shubam-disseqt/z-code-reviewer`
# opens a PR that replaces the RELEASE_* placeholders below on every tagged
# release.
class Zreview < Formula
  desc "Deterministic, LLM-free code review CLI"
  homepage "https://github.com/shubam-disseqt/z-code-reviewer"
  version "RELEASE_VERSION"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/shubam-disseqt/z-code-reviewer/releases/download/v#{version}/zreview_#{version}_darwin_arm64.tar.gz"
      sha256 "RELEASE_SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "https://github.com/shubam-disseqt/z-code-reviewer/releases/download/v#{version}/zreview_#{version}_darwin_amd64.tar.gz"
      sha256 "RELEASE_SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/shubam-disseqt/z-code-reviewer/releases/download/v#{version}/zreview_#{version}_linux_arm64.tar.gz"
      sha256 "RELEASE_SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/shubam-disseqt/z-code-reviewer/releases/download/v#{version}/zreview_#{version}_linux_amd64.tar.gz"
      sha256 "RELEASE_SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install "zreview"
  end

  test do
    output = shell_output("#{bin}/zreview version")
    assert_match "zreview", output
    assert_match version.to_s, output
  end
end
