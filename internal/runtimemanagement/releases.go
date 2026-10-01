package runtimemanagement

// Release is a reviewed official platform artifact. Requests select a version;
// callers cannot supply archive URLs, checksums, scripts or executable paths.
type Release struct {
	Runtime string `json:"runtime"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Binary  string `json:"binary"`
}

// Codex pins: https://api.github.com/repos/openai/codex/releases/tags/rust-v0.153.4
// and https://api.github.com/repos/openai/codex/releases/tags/rust-v0.159.2.
// Caelis pins: https://releases.caelis.dev/releases/v0.65.0/checksums.txt.
// Layout follows the public official installers without executing their scripts.
var officialReleases = []Release{
	{Runtime: "codex", Version: "0.153.4", Arch: "amd64", URL: "https://github.com/openai/codex/releases/download/rust-v0.153.4/codex-package-x86_64-unknown-linux-musl.tar.gz", SHA256: "a822187e1a2420c61c5926721bfbd878701ed95547c9bb0d4de4498a16ba1821", Size: 126196303, Binary: "bin/codex"},
	{Runtime: "codex", Version: "0.153.4", Arch: "arm64", URL: "https://github.com/openai/codex/releases/download/rust-v0.153.4/codex-package-aarch64-unknown-linux-musl.tar.gz", SHA256: "fc395cb043a1093ab0db34f44aba3199bfaa9ce640cd9be7fd588f44b0da64a4", Size: 117238842, Binary: "bin/codex"},
	{Runtime: "codex", Version: "0.159.2", Arch: "amd64", URL: "https://github.com/openai/codex/releases/download/rust-v0.159.2/codex-package-x86_64-unknown-linux-musl.tar.gz", SHA256: "9e2d29a713b94478b240dec2f10e11324cd05fad76dc43e7c639bdf8a1337a6b", Size: 159961162, Binary: "bin/codex"},
	{Runtime: "codex", Version: "0.159.2", Arch: "arm64", URL: "https://github.com/openai/codex/releases/download/rust-v0.159.2/codex-package-aarch64-unknown-linux-musl.tar.gz", SHA256: "05a524a463cadf7e3e22c7f923539c0d0b74c3e78b1f5f1fab52e50e6fb3312f", Size: 150270060, Binary: "bin/codex"},
	{Runtime: "caelis", Version: "0.65.0", Arch: "amd64", URL: "https://releases.caelis.dev/releases/v0.65.0/caelis_0.65.0_linux_amd64.tar.gz", SHA256: "eb540faa456b70cd43a381520afe4ea2de01d06e66d64a853ffb7a22826ce533", Size: 0, Binary: "caelis"},
	{Runtime: "caelis", Version: "0.65.0", Arch: "arm64", URL: "https://releases.caelis.dev/releases/v0.65.0/caelis_0.65.0_linux_arm64.tar.gz", SHA256: "25e5124d746c34e0496aa933f8cf33da338338a489e7e8f23489bc9879193286", Size: 0, Binary: "caelis"},
}

func Releases() []Release { return append([]Release(nil), officialReleases...) }

// Reviewed macOS artifacts use the same public checksum manifest. They are
// selected only by the trusted in-process local Node constructor; remote Linux
// installation keeps its existing releases and HOME policy.
var darwinReleases = []Release{
	{Runtime: "caelis", Version: "0.65.0", Arch: "amd64", URL: "https://releases.caelis.dev/releases/v0.65.0/caelis_0.65.0_darwin_amd64.tar.gz", SHA256: "21bb8efb68f2a569fd8ede6829ccef6f8adf2a4ec1c395e00c152e098ab0da20", Binary: "caelis"},
	{Runtime: "caelis", Version: "0.65.0", Arch: "arm64", URL: "https://releases.caelis.dev/releases/v0.65.0/caelis_0.65.0_darwin_arm64.tar.gz", SHA256: "38e7b00c45cd0920e848f00bca3b9dfe6f5290a4d78a6380bf279d56949f80e7", Binary: "caelis"},
}
