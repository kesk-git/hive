package proof

import (
	"fmt"
	"path"
	"strings"
)

// BuildFiles are the names of the build files of another toolchain than the
// repository's own, for How it is proved item 7. Makefile and go.mod are the
// repository's own and are not here.
var BuildFiles = []string{
	// Node
	"package.json", "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "pnpm-workspace.yaml",
	"bun.lockb", "bun.lock", "deno.json", "deno.jsonc", "tsconfig.json", "webpack.config.js", "rollup.config.js",
	"vite.config.js", "vite.config.ts", "gruntfile.js", "gulpfile.js",
	// Python
	"requirements.txt", "pyproject.toml", "setup.py", "setup.cfg", "pipfile", "pipfile.lock", "poetry.lock", "tox.ini", "uv.lock",
	// Rust
	"cargo.toml", "cargo.lock",
	// Java, Kotlin and the JVM
	"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradlew", "build.sbt", "build.xml",
	// C and C++
	"cmakelists.txt", "meson.build", "configure.ac", "configure", "conanfile.txt", "conanfile.py", "vcpkg.json",
	// Ruby, PHP, Elixir, Bazel and the like
	"gemfile", "gemfile.lock", "rakefile", "composer.json", "mix.exs", "workspace", "workspace.bazel", "module.bazel", "build.bazel",
}

// BuildFileSuffixes are the endings of the build files named by their kind.
var BuildFileSuffixes = []string{".csproj", ".fsproj", ".vbproj", ".sln", ".gemspec", ".cabal", ".nimble", ".podspec", ".xcodeproj", ".vcxproj"}

// OtherToolchains is How it is proved item 7: it lists every file of the
// repository tree outside .git, dist/ and test data that is a build file of
// another toolchain. Names are compared without regard to case.
func OtherToolchains(root string) ([]string, error) {
	var found []string
	err := walkFiles(root, func(rel string) error {
		name := strings.ToLower(path.Base(rel))
		match := false
		for _, b := range BuildFiles {
			match = match || name == b
		}
		for _, s := range BuildFileSuffixes {
			match = match || strings.HasSuffix(name, s)
		}
		if match {
			found = append(found, fmt.Sprintf("%s is a build file of another toolchain", rel))
		}
		return nil
	})
	return found, err
}
