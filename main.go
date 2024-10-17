package main

import (
	"fmt"

	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/utils"
)

//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=pkg/api/types.cfg.yaml api/beta.yaml
//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=pkg/api/client.cfg.yaml api/beta.yaml

func main() {
	fsys := afero.NewOsFs()
	err := utils.LoadConfigFS(fsys)
	if err != nil {
		// console log the error
		fmt.Printf("Error loading config: %v\n", err)
	}
	// console log those
	fmt.Printf("JWT Secret: %s\n", *utils.Config.Auth.JwtSecret)
	fmt.Printf("DB Root Key: %s\n", utils.Config.Db.RootKey)
	fmt.Printf("Auth url: %s\n", utils.Config.Auth.SiteUrl)
	// fmt.Printf("Environment: %s\n", utils.Config.Environment)
	// fmt.Printf("Envconfigdirect: %s\n", utils.Config.Envconfigdirect)
}
