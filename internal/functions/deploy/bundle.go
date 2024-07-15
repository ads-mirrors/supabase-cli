package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/go-errors/errors"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/pkg/function"
)

type DockerBundler struct {
	Fsys afero.Fs
}

func (b *DockerBundler) Bundle(ctx context.Context, entrypoint string, importMap string, output io.Writer) error {
	// Create temp directory to store generated eszip
	slug := filepath.Base(filepath.Dir(entrypoint))
	fmt.Fprintln(os.Stderr, "Bundling function:", utils.Bold(slug))
	hostOutputDir := filepath.Join(utils.TempDir, fmt.Sprintf(".output_%s", slug))
	// BitBucket pipelines require docker bind mounts to be world writable
	if err := b.Fsys.MkdirAll(hostOutputDir, 0777); err != nil {
		return errors.Errorf("failed to mkdir: %w", err)
	}
	defer func() {
		if err := b.Fsys.RemoveAll(hostOutputDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	cwd, err := os.Getwd()
	if err != nil {
		return errors.Errorf("failed to get working directory: %w", err)
	}
	hostFuncDir := filepath.Join(cwd, utils.FunctionsDir)
	dockerFuncDir := utils.ToDockerPath(hostFuncDir)

	outputPath := utils.DockerEszipDir + "/output.eszip"
	binds := []string{
		// Reuse deno cache directory, ie. DENO_DIR, between container restarts
		// https://denolib.gitbook.io/guide/advanced/deno_dir-code-fetch-and-cache
		utils.EdgeRuntimeId + ":/root/.cache/deno:rw",
		hostFuncDir + ":" + dockerFuncDir + ":ro",
		filepath.Join(cwd, hostOutputDir) + ":" + utils.DockerEszipDir + ":rw",
	}

	cmd := []string{"bundle", "--entrypoint", entrypoint, "--output", outputPath}
	if viper.GetBool("DEBUG") {
		cmd = append(cmd, "--verbose")
	}

	if importMap != "" {
		modules, dockerImportMapPath, err := utils.BindImportMap(importMap, b.Fsys)
		if err != nil {
			return err
		}
		binds = append(binds, modules...)
		cmd = append(cmd, "--import-map", dockerImportMapPath)
	}

	if err := utils.DockerRunOnceWithConfig(
		ctx,
		container.Config{
			Image: utils.Config.EdgeRuntime.Image,
			Env:   []string{},
			Cmd:   cmd,
		},
		container.HostConfig{
			Binds: binds,
		},
		network.NetworkingConfig{},
		"",
		os.Stdout,
		os.Stderr,
	); err != nil {
		return err
	}

	eszipBytes, err := b.Fsys.Open(filepath.Join(hostOutputDir, "output.eszip"))
	if err != nil {
		return errors.Errorf("failed to open eszip: %w", err)
	}
	defer eszipBytes.Close()
	return function.Compress(eszipBytes, output)
}
