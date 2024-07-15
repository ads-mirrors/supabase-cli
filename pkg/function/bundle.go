package function

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/andybalholm/brotli"
	"github.com/go-errors/errors"
)

type NativeBundler struct {
	BinPath string
}

func (b *NativeBundler) Bundle(ctx context.Context, entrypoint string, importMap string, output io.Writer) error {
	slug := filepath.Base(filepath.Dir(entrypoint))
	outputPath := filepath.Join(os.TempDir(), slug+".eszip")
	// TODO: make edge runtime write to stdout
	args := []string{"bundle", "--entrypoint", entrypoint, "--output", outputPath}
	if len(importMap) > 0 {
		args = append(args, "--import-map", importMap)
	}
	cmd := exec.CommandContext(ctx, b.BinPath, args...)
	if err := cmd.Run(); err != nil {
		return errors.Errorf("failed to bundle function: %w", err)
	}
	defer os.Remove(outputPath)
	// Compress the output
	eszipBytes, err := os.Open(outputPath)
	if err != nil {
		return errors.Errorf("failed to open eszip: %w", err)
	}
	defer eszipBytes.Close()
	return Compress(eszipBytes, output)
}

const compressedEszipMagicID = "EZBR"

func Compress(r io.Reader, w io.Writer) error {
	if _, err := fmt.Fprint(w, compressedEszipMagicID); err != nil {
		return errors.Errorf("failed to append magic id: %w", err)
	}
	brw := brotli.NewWriter(w)
	defer brw.Close()
	if _, err := io.Copy(brw, r); err != nil {
		return errors.Errorf("failed to compress eszip: %w", err)
	}
	return nil
}
