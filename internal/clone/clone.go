package clone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/supabase/cli/internal/db/pull"
	"github.com/supabase/cli/internal/link"
	"github.com/supabase/cli/internal/login"
	"github.com/supabase/cli/internal/migration/new"
	"github.com/supabase/cli/internal/migration/repair"
	"github.com/supabase/cli/internal/projects/apiKeys"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/internal/utils/flags"
	"github.com/supabase/cli/internal/utils/tenant"
	"github.com/supabase/cli/pkg/api"
	"golang.org/x/term"
)

func Run(ctx context.Context, fsys afero.Fs) error {
	if err := changeWorkDir(ctx, fsys); err != nil {
		return err
	}
	// 1. Login
	if err := checkLogin(ctx, fsys); err != nil {
		return err
	}
	// 2. Link project
	if err := linkProject(ctx, fsys); err != nil {
		return err
	}
	// 3. Pull migrations
	dbConfig := flags.NewDbConfigWithPassword(ctx, flags.ProjectRef)
	if err := dumpRemoteSchema(ctx, dbConfig, fsys); err != nil {
		return err
	}
	return nil
}

func changeWorkDir(ctx context.Context, fsys afero.Fs) error {
	workdir := viper.GetString("WORKDIR")
	if !filepath.IsAbs(workdir) {
		workdir = filepath.Join(utils.CurrentDirAbs, workdir)
	}
	if err := utils.MkdirIfNotExistFS(fsys, workdir); err != nil {
		return err
	}
	if empty, err := afero.IsEmpty(fsys, workdir); err != nil {
		return errors.Errorf("failed to read workdir: %w", err)
	} else if !empty {
		title := fmt.Sprintf("Do you want to overwrite existing files in %s directory?", utils.Bold(workdir))
		if shouldOverwrite, err := utils.NewConsole().PromptYesNo(ctx, title, true); err != nil {
			return err
		} else if !shouldOverwrite {
			return errors.New(context.Canceled)
		}
	}
	return utils.ChangeWorkDir(fsys)
}

func checkLogin(ctx context.Context, fsys afero.Fs) error {
	if _, err := utils.LoadAccessTokenFS(fsys); !errors.Is(err, utils.ErrMissingToken) {
		return err
	}
	params := login.RunParams{
		OpenBrowser: term.IsTerminal(int(os.Stdin.Fd())),
		Fsys:        fsys,
	}
	return login.Run(ctx, os.Stdout, params)
}

func linkProject(ctx context.Context, fsys afero.Fs) error {
	// Use an empty fs to skip loading from file
	if err := flags.ParseProjectRef(ctx, afero.NewMemMapFs()); err != nil {
		return err
	}
	policy := utils.NewBackoffPolicy(ctx)
	keys, err := backoff.RetryNotifyWithData(func() ([]api.ApiKeyResponse, error) {
		fmt.Fprintln(os.Stderr, "Linking project...")
		return apiKeys.RunGetApiKeys(ctx, flags.ProjectRef)
	}, policy, utils.NewErrorCallback())
	if err != nil {
		return err
	}
	// Load default config to update docker id
	if err := flags.LoadConfig(fsys); err != nil {
		return err
	}
	link.LinkServices(ctx, flags.ProjectRef, tenant.NewApiKey(keys).ServiceRole, fsys)
	return utils.WriteFile(utils.ProjectRefPath, []byte(flags.ProjectRef), fsys)
}

func dumpRemoteSchema(ctx context.Context, config pgconn.Config, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	// 1. Check postgres connection
	conn, err := utils.ConnectByConfig(ctx, config, options...)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	// 2. Pull schema
	timestamp := utils.GetCurrentTimestamp()
	path := new.GetMigrationPath(timestamp, "remote_schema")
	// Ignore schemas flag when working on the initial pull
	if err = pull.CloneRemoteSchema(ctx, path, config, fsys); err != nil {
		return err
	}
	// 3. Insert a row to `schema_migrations`
	fmt.Fprintln(os.Stderr, "Schema written to "+utils.Bold(path))
	if shouldUpdate, err := utils.NewConsole().PromptYesNo(ctx, "Update remote migration history table?", true); err != nil {
		return err
	} else if shouldUpdate {
		return repair.UpdateMigrationTable(ctx, conn, []string{timestamp}, repair.Applied, true, fsys)
	}
	return nil
}
