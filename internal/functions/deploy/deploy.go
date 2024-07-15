package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/pkg/config"
	"github.com/supabase/cli/pkg/function"
)

func Run(ctx context.Context, slugs []string, projectRef string, noVerifyJWT *bool, importMapPath string, fsys afero.Fs) error {
	// Load function config and project id
	if err := utils.LoadConfigFS(fsys); err != nil {
		return err
	} else if len(slugs) > 0 {
		for _, s := range slugs {
			if err := utils.ValidateFunctionSlug(s); err != nil {
				return err
			}
		}
	} else if slugs, err = GetFunctionSlugs(fsys); err != nil {
		return err
	}
	// TODO: only deploy from functions config in v2
	if len(slugs) == 0 {
		return errors.Errorf("No Functions specified or found in %s", utils.Bold(utils.FunctionsDir))
	}
	fallbackExists := true
	if _, err := fsys.Stat(utils.FallbackImportMapPath); errors.Is(err, os.ErrNotExist) {
		fallbackExists = false
	} else if err != nil {
		return errors.Errorf("failed to fallback import map: %w", err)
	}
	functionConfig := make(config.FunctionConfig, len(slugs))
	for _, s := range slugs {
		function := utils.Config.Functions[s]
		// TODO: support entrypoint override for index.js
		if len(function.Entrypoint) == 0 {
			function.Entrypoint = filepath.Join("functions", s, "index.ts")
		}
		if len(importMapPath) > 0 {
			if !filepath.IsAbs(importMapPath) {
				importMapPath = filepath.Join(utils.CurrentDirAbs, importMapPath)
			}
			function.ImportMap = importMapPath
		} else if fallbackExists {
			function.ImportMap = filepath.Join("functions", "import_map.json")
		}
		if noVerifyJWT != nil {
			function.VerifyJWT = utils.Ptr(!*noVerifyJWT)
		}
		functionConfig[s] = function
	}
	api := function.NewEdgeRuntimeAPI(projectRef, *utils.GetSupabase(), &DockerBundler{Fsys: fsys})
	if err := api.UpsertFunctions(ctx, functionConfig); err != nil {
		return err
	}
	fmt.Printf("Deployed Functions on project %s: %s\n", utils.Aqua(projectRef), strings.Join(slugs, ", "))
	url := fmt.Sprintf("%s/project/%v/functions", utils.GetSupabaseDashboardURL(), projectRef)
	fmt.Println("You can inspect your deployment in the Dashboard: " + url)
	return nil
}

func GetFunctionSlugs(fsys afero.Fs) ([]string, error) {
	pattern := filepath.Join(utils.FunctionsDir, "*", "index.ts")
	paths, err := afero.Glob(fsys, pattern)
	if err != nil {
		return nil, errors.Errorf("failed to glob function slugs: %w", err)
	}
	var slugs []string
	for _, path := range paths {
		slug := filepath.Base(filepath.Dir(path))
		if utils.FuncSlugPattern.MatchString(slug) {
			slugs = append(slugs, slug)
		}
	}
	return slugs, nil
}
