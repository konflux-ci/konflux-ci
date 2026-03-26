package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/konflux-ci/konflux-ci/operator/pkg/overrides"
)

func main() {
	var (
		upstreamDir  string
		manifestsDir string
		tmpDir       string
		overridesYML string
	)

	flag.StringVar(&upstreamDir, "upstream-dir", "", "Path to upstream-kustomizations directory")
	flag.StringVar(&manifestsDir, "manifests-dir", "", "Path to manifests directory")
	flag.StringVar(&tmpDir, "tmp-dir", "", "Path to temp working directory")
	flag.StringVar(&overridesYML, "overrides-yaml", "", "Inline overrides YAML content")
	flag.Parse()

	if upstreamDir == "" {
		fmt.Fprintln(os.Stderr, "error: --upstream-dir is required")
		os.Exit(1)
	}
	if manifestsDir == "" {
		fmt.Fprintln(os.Stderr, "error: --manifests-dir is required")
		os.Exit(1)
	}
	if tmpDir == "" {
		fmt.Fprintln(os.Stderr, "error: --tmp-dir is required")
		os.Exit(1)
	}
	if overridesYML == "" {
		fmt.Fprintln(os.Stderr, "error: --overrides-yaml is required")
		os.Exit(1)
	}

	overridesConfig, err := overrides.ParseAndValidateFromYAML(overridesYML)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	runner, err := overrides.NewRunner(upstreamDir, manifestsDir, tmpDir, overridesConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := runner.Apply(); err != nil {
		fmt.Fprintf(os.Stderr, "error: apply overrides: %v\n", err)
		os.Exit(1)
	}

	if lines := runner.GitSummaryLines(); len(lines) > 0 {
		fmt.Println("")
		fmt.Println("Configured git overrides:")
		for _, line := range lines {
			fmt.Println(line)
		}
	}
	if lines := runner.SummaryLines(); len(lines) > 0 {
		fmt.Println("")
		fmt.Println("Applied image overrides:")
		for _, line := range lines {
			fmt.Println(line)
		}
	}
	st := runner.Stats()
	fmt.Println("")
	fmt.Printf(
		"Apply summary: git kustomizations=%d, kustomization image patches=%d, "+
			"manifest YAML image replacements=%d, components rebuilt=%d\n",
		st.GitKustomizationsUpdated, st.KustomizationImagesPatched,
		st.ManifestYAMLsImageTextReplaced, st.ComponentsRebuilt,
	)
}
