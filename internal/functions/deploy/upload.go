package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/go-errors/errors"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/internal/utils/flags"
	"github.com/supabase/cli/pkg/api"
	"github.com/supabase/cli/pkg/cast"
	"github.com/supabase/cli/pkg/config"
)

func deploy(ctx context.Context, functionConfig config.FunctionConfig, fsys afero.Fs) error {
	for slug, fc := range functionConfig {
		if !fc.IsEnabled() {
			fmt.Fprintln(os.Stderr, "Skipped deploying Function:", slug)
			continue
		}
		fmt.Fprintln(os.Stderr, "Deploying Function:", slug)
		meta := api.FunctionMetadata{
			Name:           &slug,
			EntrypointPath: fc.Entrypoint,
			ImportMapPath:  &fc.ImportMap,
			VerifyJwt:      fc.VerifyJWT,
		}
		if len(fc.StaticFiles) > 0 {
			meta.StaticPatterns = &fc.StaticFiles
		}
		if err := upload(ctx, slug, meta, fsys); err != nil {
			return err
		}
	}
	return nil
}

func upload(ctx context.Context, slug string, meta api.FunctionMetadata, fsys afero.Fs) error {
	body, w := io.Pipe()
	form := multipart.NewWriter(w)
	errchan := make(chan error, 1)
	go func() {
		defer close(errchan)
		defer w.Close()
		defer form.Close()
		if err := writeForm(form, meta, fsys); err != nil {
			errchan <- err
		}
	}()
	resp, err := utils.GetSupabase().V1DeployAFunctionWithBodyWithResponse(
		ctx,
		flags.ProjectRef,
		&api.V1DeployAFunctionParams{Slug: &slug},
		form.FormDataContentType(),
		body,
	)
	if merr := <-errchan; merr != nil {
		return err
	} else if err != nil {
		return errors.Errorf("failed to deploy function: %w", err)
	} else if resp.JSON201 == nil {
		return errors.Errorf("unexpected deploy status %d: %s", resp.StatusCode(), string(resp.Body))
	}
	return nil
}

func writeForm(form *multipart.Writer, meta api.FunctionMetadata, fsys afero.Fs) error {
	m, err := form.CreateFormField("metadata")
	if err != nil {
		return errors.Errorf("failed to create metadata: %w", err)
	}
	enc := json.NewEncoder(m)
	if err := enc.Encode(meta); err != nil {
		return errors.Errorf("failed to encode metadata: %w", err)
	}
	addFile := func(srcPath string, data []byte) error {
		f, err := form.CreateFormFile("file", srcPath)
		if err != nil {
			return errors.Errorf("failed to create file: %w", err)
		}
		if _, err := f.Write(data); err != nil {
			return errors.Errorf("failed to write file: %w", err)
		}
		return nil
	}
	// Add import map
	importMap := utils.ImportMap{}
	if imPath := cast.Val(meta.ImportMapPath, ""); len(imPath) > 0 {
		data, err := afero.ReadFile(fsys, filepath.FromSlash(imPath))
		if err != nil {
			return errors.Errorf("failed to load import map: %w", err)
		}
		if err := importMap.Parse(data); err != nil {
			return err
		}
		if err := addFile(imPath, data); err != nil {
			return err
		}
	}
	// Add static files
	for _, pattern := range cast.Val(meta.StaticPatterns, []string{}) {
		matches, err := afero.Glob(fsys, pattern)
		if err != nil {
			return errors.Errorf("failed to glob files: %w", err)
		}
		for _, sfPath := range matches {
			data, err := afero.ReadFile(fsys, filepath.FromSlash(sfPath))
			if errors.Is(err, syscall.EISDIR) {
				fmt.Fprintln(os.Stderr, utils.Yellow("WARNING:"), err)
				continue
			} else if err != nil {
				return errors.Errorf("failed to load static file: %w", err)
			}
			if err := addFile(sfPath, data); err != nil {
				return err
			}
		}
	}
	return walkImportPaths(meta.EntrypointPath, importMap, fsys, addFile)
}

// Ref: https://regex101.com/r/DfBdJA/1
var importPathPattern = regexp.MustCompile(`(?i)import\s+(?:{[^{}]+}|.*?)\s*(?:from)?\s*['"](.*?)['"]|import\(\s*['"](.*?)['"]\)`)

func walkImportPaths(srcPath string, importMap utils.ImportMap, fsys afero.Fs, callback func(curr string, data []byte) error) error {
	seen := map[string]struct{}{}
	// BFS so we can list paths in increasing depth
	q := make([]string, 1)
	q[0] = srcPath
	for len(q) > 0 {
		curr := q[len(q)-1]
		q = q[:len(q)-1]
		// Assume no file is symlinked
		if _, ok := seen[curr]; ok {
			continue
		}
		data, err := afero.ReadFile(fsys, filepath.FromSlash(curr))
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, utils.Yellow("WARNING:"), err)
			continue
		} else if err != nil {
			return errors.Errorf("failed to read file: %w", err)
		}
		seen[curr] = struct{}{}
		if err := callback(curr, data); err != nil {
			return err
		}
		// Traverse all modules imported by the current source file
		for _, matches := range importPathPattern.FindAllStringSubmatch(string(data), -1) {
			if len(matches) < 3 {
				continue
			}
			mod := matches[1]
			if len(mod) == 0 {
				mod = matches[2]
			}
			mod = strings.TrimSpace(mod)
			// Substitute kv from import map
			for k, v := range importMap.Imports {
				if strings.HasPrefix(mod, k) {
					mod = v + mod[len(k):]
				}
			}
			if strings.HasPrefix(mod, "./") || strings.HasPrefix(mod, "../") {
				mod = path.Join(path.Dir(curr), mod)
			} else if !strings.HasPrefix(mod, "/") {
				continue
			}
			q = append(q, mod)
		}
	}
	return nil
}
