package build

import (
	"encoding/json"
	"fmt"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/ociregistry"
)

type Sbom interface {
	GetPackages() []SbomPackage
}

type SbomPackage interface {
	GetName() string
	GetVersion() string
	GetPurl() string
	GetCreatedBy() SbomPackageCreatedBy
}

type SbomPackageCreatedBy int

const (
	SbomPackageCreatedByUnknown SbomPackageCreatedBy = iota
	SbomPackageCreatedByHermeto
)

type SbomCyclonedx struct {
	BomFormat   string
	SpecVersion string
	Version     int
	Components  []CyclonedxComponent `json:"components"`
}

type CyclonedxComponent struct {
	Name       string              `json:"name"`
	Purl       string              `json:"purl"`
	Type       string              `json:"type"`
	Version    string              `json:"version"`
	Properties []CyclonedxProperty `json:"properties"`
}

type CyclonedxProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (s *SbomCyclonedx) GetPackages() []SbomPackage {
	packages := []SbomPackage{}
	for i := range s.Components {
		packages = append(packages, &s.Components[i])
	}
	return packages
}

func (c *CyclonedxComponent) GetName() string {
	return c.Name
}

func (c *CyclonedxComponent) GetVersion() string {
	return c.Version
}

func (c *CyclonedxComponent) GetPurl() string {
	return c.Purl
}

func (c *CyclonedxComponent) GetCreatedBy() SbomPackageCreatedBy {
	for _, property := range c.Properties {
		if property.Name == "hermeto:found_by" && property.Value == "hermeto" {
			return SbomPackageCreatedByHermeto
		}
	}
	return SbomPackageCreatedByUnknown
}

type SbomSpdx struct {
	SPDXID      string        `json:"SPDXID"`
	SpdxVersion string        `json:"spdxVersion"`
	Packages    []SpdxPackage `json:"packages"`
}

type SpdxPackage struct {
	Name         string            `json:"name"`
	VersionInfo  string            `json:"versionInfo"`
	ExternalRefs []SpdxExternalRef `json:"externalRefs"`
	Annotations  []SpdxAnnotation  `json:"annotations"`
}

type SpdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceLocator  string `json:"referenceLocator"`
	ReferenceType     string `json:"referenceType"`
}

type SpdxAnnotation struct {
	Annotator string `json:"annotator"`
	Comment   string `json:"comment"`
}

func (s *SbomSpdx) GetPackages() []SbomPackage {
	packages := []SbomPackage{}
	for i := range s.Packages {
		packages = append(packages, &s.Packages[i])
	}
	return packages
}

func (p *SpdxPackage) GetName() string {
	return p.Name
}

func (p *SpdxPackage) GetVersion() string {
	return p.VersionInfo
}

func (p *SpdxPackage) GetPurl() string {
	for _, ref := range p.ExternalRefs {
		if ref.ReferenceType == "purl" {
			return ref.ReferenceLocator
		}
	}
	return ""
}

func (p *SpdxPackage) GetCreatedBy() SbomPackageCreatedBy {
	for _, ref := range p.Annotations {
		if ref.Annotator == "Tool: hermeto:jsonencoded" {
			return SbomPackageCreatedByHermeto
		}
	}
	return SbomPackageCreatedByUnknown
}

func UnmarshalSbom(data []byte) (Sbom, error) {
	cdx := SbomCyclonedx{}
	if err := json.Unmarshal(data, &cdx); err != nil {
		return nil, fmt.Errorf("unmarshalling SBOM: %w", err)
	}
	if cdx.BomFormat != "" {
		return &cdx, nil
	}

	spdx := SbomSpdx{}
	if err := json.Unmarshal(data, &spdx); err != nil {
		return nil, fmt.Errorf("unmarshalling SBOM: %w", err)
	}
	if spdx.SPDXID != "" {
		return &spdx, nil
	}

	return nil, fmt.Errorf("unmarshalling SBOM: doesn't look like either CycloneDX or SPDX")
}

func FetchSbomFromRegistry(c *ociregistry.OciRegistryV2Client, organization, repository, sbomBlobDigest string) (Sbom, error) {
	blobData, err := c.FetchBlob(organization, repository, sbomBlobDigest)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blob: %w", err)
	}

	sbom, err := UnmarshalSbom(blobData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sbom: %w", err)
	}

	return sbom, nil
}
