package main

import "github.com/sharedcode/sop/tools/runtimehub"

type ModelCatalog = runtimehub.ModelCatalog
type ModelCatalogGroup = runtimehub.ModelCatalogGroup
type ModelCatalogOption = runtimehub.ModelCatalogOption

var (
	modelCatalog     = runtimehub.DefaultModelCatalog()
	modelCatalogPath string
)

func defaultModelCatalog() ModelCatalog {
	return runtimehub.DefaultModelCatalog()
}

func ensureModelCatalogDefaults(catalog *ModelCatalog) bool {
	return runtimehub.EnsureModelCatalogDefaults(catalog)
}

func resolveModelCatalogPath(configPath string) string {
	return runtimehub.ResolveModelCatalogPath(configPath)
}

func modelCatalogCandidatePaths(configPath string) []string {
	return runtimehub.CandidateModelCatalogPaths(configPath)
}

func loadModelCatalog(configPath string) (ModelCatalog, error) {
	catalog, path, err := runtimehub.LoadModelCatalog(configPath)
	if err != nil {
		return modelCatalog, err
	}
	modelCatalog = catalog
	modelCatalogPath = path
	return modelCatalog, nil
}
