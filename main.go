package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	semver2 "github.com/Masterminds/semver"
	"github.com/alex-held/dfctl-kit/pkg/dflog"
	"github.com/alex-held/dfctl-kit/pkg/env"
	"github.com/alex-held/dfctl-kit/pkg/iostreams"
	"github.com/alex-held/dfctl-kit/pkg/system"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

const DownloadURL = "https://golang.org"

var InstallPath = filepath.Join(env.SDKs(), "go")

var errOnlyOsFsSupported = errors.New("only afero.OsFs is supported")
var errNoCurrentVersion = errors.New("current version is not linked")
var ErrVersionNotInstalled = errors.New("go version is not installed locally")

type executor struct {
	afero.Fs
	Streams     *iostreams.IOStreams
	URL         string
	InstallPath string
}

func defaultExecutor() *executor {
	return &executor{
		Fs:          afero.NewOsFs(),
		Streams:     iostreams.Default(),
		URL:         DownloadURL,
		InstallPath: InstallPath,
	}
}

func main() {
	dflog.Configure()

	cmd := NewCmd()
	err := cmd.Execute()
	if err != nil {
		log.Fatal().Err(err).Send()
	}
}

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dfctl-go",
		Short: "manages and installs go sdks",
		RunE: func(c *cobra.Command, args []string) error {
			return c.Help()
		},
		Version: fmt.Sprintf("devctl-go version %v", version),
	}

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "installs the provided version of the go sdk",
		RunE: func(c *cobra.Command, args []string) error {
			if err := validateArgsForSubcommand("install", args, 1); err != nil {
				return err
			}
			e := defaultExecutor()

			version, err := ParseVersion(args[0])
			if err != nil {
				return err
			}
			return e.Install(version)
		},
	}
	useCmd := &cobra.Command{
		Use:   "use",
		Short: "sets a go sdk version as the system default",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateArgsForSubcommand("use", args, 1); err != nil {
				return err
			}
			e := defaultExecutor()
			version, err := ParseVersion(args[0])
			if err != nil {
				return err
			}
			return e.Use(version)
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "lists installed go sdks",
		RunE: func(c *cobra.Command, args []string) error {
			if err := validateArgsForSubcommand("list", args, 0); err != nil {
				return err
			}
			e := defaultExecutor()
			return e.List()
		},
	}

	currentCmd := &cobra.Command{
		Use:   "current",
		Short: "prints the currently installed go version",
		RunE: func(c *cobra.Command, args []string) error {
			if err := validateArgsForSubcommand("current", args, 0); err != nil {
				return err
			}
			e := defaultExecutor()
			return e.Current()
		},
	}

	cmd.AddCommand(currentCmd)
	cmd.AddCommand(listCmd)
	cmd.AddCommand(installCmd)
	cmd.AddCommand(useCmd)

	return cmd
}

func MustParseVersion(s string) Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}
func ParseVersion(s string) (Version, error) {
	v, err := semver2.NewVersion(s)
	if err != nil {
		return "", err
	}
	return Version(v.String()), nil
}

func (e *executor) Install(version Version) error {
	installPath := path.Join(e.InstallPath, version.String())
	archive, err := e.dlArchive(version)
	if err != nil {
		return err
	}

	log.Debug().Msgf("downloaded %v to path %v", version.String(), installPath)
	err = e.Fs.MkdirAll(installPath, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create install directory at %s; %w", installPath, err)
	}
	err = unTarGzip(archive, installPath, unarchiveRenamer(), e.Fs)
	if err != nil {
		return fmt.Errorf("failed to Extract go sdk %s; dest=%s; archive=%s;err=%v\n", version, installPath, "*Bytes.Buffer", err)
	}
	return nil
}

func (e *executor) Use(version Version) error {
	versionPath := filepath.Join(e.InstallPath, version.String())
	currentPath := filepath.Join(e.InstallPath, "current")

	osFs, ok := e.Fs.(*afero.OsFs)
	if !ok {
		return errOnlyOsFsSupported
	}

	if exists, err := afero.DirExists(osFs, versionPath); err != nil || !exists {
		return ErrVersionNotInstalled
	}

	_ = osFs.Remove(currentPath)
	if err := osFs.SymlinkIfPossible(versionPath, currentPath); err != nil {
		return err
	}
	return nil
}

type Version string

func (v Version) Number() string {
	return strings.TrimPrefix(string(v), "v")
}

func (v Version) String() string {
	return string(v)
}

func (e *executor) list() (versions []Version, err error) {
	fis, err := afero.ReadDir(e.Fs, e.InstallPath)
	if err != nil {
		return versions, err
	}
	for _, fi := range fis {
		if fi.IsDir() {
			versions = append(versions, Version(fi.Name()))
		}
	}

	return versions, nil
}

func (e *executor) List() error {
	versions, err := e.list()
	if err != nil {
		return err
	}
	for _, version := range versions {
		_, _ = fmt.Fprintln(e.Streams.Out, version.String())
	}
	return nil
}

func (e *executor) current() (Version, error) {
	installPath := filepath.Join(e.InstallPath, "current")
	osFs, ok := e.Fs.(*afero.OsFs)
	if !ok {
		return Version(""), errOnlyOsFsSupported
	}

	link, err := osFs.ReadlinkIfPossible(installPath)
	if err != nil {
		return Version(""), errNoCurrentVersion
	}

	currentDir := path.Base(link)
	currentVersion, err := ParseVersion(currentDir)
	if err != nil {
		return Version(""), err
	}
	return currentVersion, nil
}

func (e *executor) Current() error {
	currentVersion, err := e.current()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(e.Streams.Out, currentVersion.String())
	return nil
}

func validateArgsForSubcommand(subcmd string, args []string, expected int) error {
	if len(args) != expected {
		return fmt.Errorf("provided wrong number of argument for subcommand '%s'; expected=%d; provided=%d", subcmd, expected, len(args))
	}
	return nil
}

func formatGoArchiveArtifactName(ri system.RuntimeInfo, version string) string {
	return ri.Format("go%s.[os]-[arch].tar.gz", version)
}

func (e *executor) dlArchive(version Version) (archive *bytes.Buffer, err error) {
	ri := system.OSRuntimeInfoGetter{}
	artifactName := formatGoArchiveArtifactName(ri.Get(), version.String())
	dlUri := ri.Get().Format("%s/dl/%s", e.URL, artifactName)

	buf := &bytes.Buffer{}
	err = e.download(context.Background(), dlUri, buf)
	if err != nil {
		return buf, fmt.Errorf("failed downloading go sdk %v from the remote server %s; err=%v", version, "https://golang.org", err)
	}

	return buf, nil
}

func (e *executor) download(ctx context.Context, url string, outWriter io.Writer) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(outWriter, resp.Body)
	return err
}

func unTarGzip(buf *bytes.Buffer, target string, renamer Renamer, fs afero.Fs) error {
	gr, _ := gzip.NewReader(buf)
	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()

		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		filename := header.Name
		if renamer != nil {
			filename = renamer(filename)
		}

		p := filepath.Join(target, filename)
		fi := header.FileInfo()

		if fi.IsDir() {
			if e := fs.MkdirAll(p, fi.Mode()); e != nil {
				return e
			}
			continue
		}
		file, err := fs.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fi.Mode())
		if err != nil {
			return err
		}

		_, err = io.Copy(file, tr)
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type Renamer func(p string) string

func unarchiveRenamer() Renamer {
	return func(p string) string {
		parts := strings.Split(p, string(filepath.Separator))
		parts = parts[1:]
		newPath := strings.Join(parts, string(filepath.Separator))
		return newPath
	}
}
