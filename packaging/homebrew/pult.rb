# Template: scripts/render-packaging.sh fills the @...@ fields from a release.
# Builds from source, so no Apple Developer ID or notarization is needed.
class Pult < Formula
  desc "Apply one palette across your terminal apps, and undo every change"
  homepage "https://git.subcult.tv/subculture-collective/pult"
  url "@BASE@pult-@VERSION@-src.tar.gz"
  sha256 "@SHA_SRC@"
  license all_of: ["GPL-3.0-only", "MIT"]

  depends_on "go" => :build

  resource "catalog" do
    url "@BASE@pult-@VERSION@-catalog.db.zst"
    sha256 "@SHA_CATALOG@"
  end

  def install
    resource("catalog").stage { cp Dir["*"].first, buildpath/"builtin/catalog.db.zst" }
    ENV["CGO_ENABLED"] = "0"
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version} -X git.subcult.tv/subculture-collective/pult.BuiltinVersion=@DATAVERSION@", output: bin/"pult"), "./cmd/pult"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/pult version")
    assert_match "catalog included", shell_output("#{bin}/pult doctor -state #{testpath}/state")
  end
end
