class Forge < Formula
  desc "AI coding task orchestrator — drives Claude Code, Gemini CLI, and Kiro in Ralph-style loops"
  homepage "https://github.com/arturklasa/forge"
  version "0.1.0"

  on_macos do
    on_arm do
      url "https://github.com/arturklasa/forge/releases/download/v#{version}/forge_darwin_arm64"
      sha256 "PLACEHOLDER_SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "https://github.com/arturklasa/forge/releases/download/v#{version}/forge_darwin_amd64"
      sha256 "PLACEHOLDER_SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/arturklasa/forge/releases/download/v#{version}/forge_linux_arm64"
      sha256 "PLACEHOLDER_SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/arturklasa/forge/releases/download/v#{version}/forge_linux_amd64"
      sha256 "PLACEHOLDER_SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install stable.url.split("/").last => "forge"
  end

  test do
    assert_match "forge", shell_output("#{bin}/forge --version")
    system "#{bin}/forge", "--help"
  end
end
