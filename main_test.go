package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"testing"

	"github.com/alex-held/dfctl-kit/pkg/testutils"
	"github.com/alex-held/dfctl-kit/pkg/testutils/matchers"
	"github.com/franela/goblin"
	. "github.com/onsi/gomega"
	"github.com/spf13/afero"
)

//go:embed testdata/go.tar.gz
var archiveData []byte

func installPath(t *testing.T) string {
	path := filepath.Join(testutils.TempDir(t), "dfctl", "sdk", "go")
	return path
}

func TestHandleList(t *testing.T) {
	testutils.Run(t, "List", func(g *goblin.G) {
		InstallPath = installPath(t)
		InstallPath = "/Users/dev/.devctl/sdks/go"

		g.BeforeEach(func() {
			createVersionDirs()
		})

		g.AfterEach(func() {
			_ = afero.NewOsFs().RemoveAll(InstallPath)
		})

		g.Describe("with installed versions", func() {

			g.It("output", func() {
				sut := defaultExecutor()
				Ω(sut.List()).Should(Succeed())
			})

			g.Describe("with current", func() {

				g.Before(func() {
					symlink(afero.NewOsFs(), filepath.Join(InstallPath, Versions[0].String()), filepath.Join(InstallPath, "current"))
				})

				g.It("doesn't list current", func() {
					sut := defaultExecutor()
					got, err := sut.list()
					Ω(err).Should(Succeed())
					Ω(got).Should(Equal(Versions))
				})
			})

			g.Describe("without current", func() {

				g.It("doesn't list current", func() {
					sut := defaultExecutor()
					got, err := sut.list()
					Ω(err).Should(Succeed())
					Ω(got).Should(Equal(Versions))
				})
			})
		})
	})
}

func createVersionDirs() {
	for _, version := range Versions {
		versionPath := filepath.Join(InstallPath, version.String())
		_ = afero.NewOsFs().MkdirAll(versionPath, os.ModePerm)
	}
}

var Versions = []Version{
	Version("v1.13.5"),
	Version("v1.16"),
	Version("v1.16.3"),
	Version("v1.16.4"),
	Version("v1.16.8"),
	Version("v1.17"),
	Version("v1.17.1"),
}

func symlink(fs afero.Fs, oldName, newName string) {
	symFs := fs.(afero.Symlinker)
	_ = symFs.SymlinkIfPossible(oldName, newName)
}

type Buffer struct {
	*bytes.Buffer
}

func (b *Buffer) Close() error { return nil }

func TestHandleCurrent(t *testing.T) {

	testutils.Run(t, "List", func(g *goblin.G) {
		InstallPath = installPath(t)

		g.BeforeEach(func() {
			createVersionDirs()
		})

		g.AfterEach(func() {
			_ = afero.NewOsFs().RemoveAll(InstallPath)
		})

		g.Describe("with linked current version", func() {
			currentVersion := MustParseVersion("v1.16.8")

			g.JustBeforeEach(func() {
				symlink(afero.NewOsFs(), filepath.Join(InstallPath, currentVersion.String()), filepath.Join(InstallPath, "current"))
			})

			g.It("returns currentVersion", func() {
				sut := defaultExecutor()
				version, err := sut.current()
				Ω(err).Should(Succeed())
				Ω(version).Should(Equal(currentVersion))
			})

			g.It("output", func() {
				sut := defaultExecutor()
				out := &Buffer{&bytes.Buffer{}}
				sut.Streams.Out = out
				_ = sut.Current()
				Ω(out.String()).Should(Equal(currentVersion.String()))
			})
		})

		g.Describe("without linked current version", func() {

			g.It("returns error", func() {
				sut := defaultExecutor()
				_, err := sut.current()
				Ω(err).Should(Equal(errNoCurrentVersion))
			})

			g.It("output", func() {
				sut := defaultExecutor()
				out := &Buffer{&bytes.Buffer{}}
				sut.Streams.Out = out
				_ = sut.Current()
				Ω(out.String()).Should(BeEmpty())
			})
		})
	})
}

func TestHandleInstall(t *testing.T) {
	testutils.Run(t, "Install", func(g *goblin.G) {
		InstallPath = installPath(t)
		const version = Version("v1.17.1")

		g.BeforeEach(func() {
			_ = os.MkdirAll(InstallPath, os.ModePerm)
		})

		g.AfterEach(func() {
			_ = os.RemoveAll(InstallPath)
		})

		g.Describe("version not installed yet", func() {
			g.It("installs version", func() {
				sut := defaultExecutor()
				Ω(sut.Install(version)).Should(Succeed())
				Ω(filepath.Join(InstallPath, version.String())).Should(BeADirectory())
			})
		})

		g.Describe("version already installed", func() {
			g.Before(func() {
				_ = os.MkdirAll(filepath.Join(InstallPath, version.String()), os.ModePerm)
			})

			g.It("should not fail", func() {
				sut := defaultExecutor()
				Ω(sut.Install(version)).Should(Succeed())
				Ω(filepath.Join(InstallPath, version.String())).Should(BeADirectory())
			})
		})
	})
}

func TestHandleUse(t *testing.T) {

	testutils.Run(t, "Use", func(g *goblin.G) {
		InstallPath = installPath(t)

		g.BeforeEach(func() {
			createVersionDirs()
		})

		g.AfterEach(func() {
			_ = os.RemoveAll(InstallPath)
		})

		g.Describe("with installed version", func() {
			const version = Version("v1.17.1")

			g.It("link version to current", func() {
				sut := defaultExecutor()
				Ω(sut.Use(version)).Should(Succeed())
				versionPath, err := os.Readlink(filepath.Join(InstallPath, "current"))
				Ω(err).Should(Succeed())
				Ω(versionPath).Should(BeADirectory())
				Ω(versionPath).Should(matchers.BeNamedFileOrDir(version.String()))
			})
		})

		g.Describe("with not installed version", func() {
			const version = Version("v99.99.99")

			g.It("return ErrVersionNotInstalled", func() {
				sut := defaultExecutor()

				Ω(sut.Use(version)).Should(Equal(ErrVersionNotInstalled))
				Ω(filepath.Join(InstallPath, "current")).ShouldNot(BeAnExistingFile())
			})
		})
	})
}
