package contract

import (
	ecp "github.com/conforma/crds/api/v1alpha1"
)

// PolicySpecWithSourceConfig returns a new EnterpriseContractPolicySpec which is a deep copy of
// the provided spec with each source config updated.
func PolicySpecWithSourceConfig(spec ecp.EnterpriseContractPolicySpec, sourceConfig ecp.SourceConfig) ecp.EnterpriseContractPolicySpec {
	var sources []ecp.Source
	for _, s := range spec.Sources {
		source := s.DeepCopy()
		source.Config = sourceConfig.DeepCopy()
		sources = append(sources, *source)
	}

	newSpec := *spec.DeepCopy()
	newSpec.Sources = sources
	return newSpec
}

// PolicySpecWithSource returns a new EnterpriseContractPolicySpec which is a deep copy of the provided spec with each source updated.
func PolicySpecWithSource(spec ecp.EnterpriseContractPolicySpec, ecpSource ecp.Source) ecp.EnterpriseContractPolicySpec {
	var sources []ecp.Source
	for _, s := range spec.Sources {
		source := s.DeepCopy()
		source.Config = ecpSource.Config
		source.RuleData = ecpSource.RuleData
		sources = append(sources, *source)
	}

	newSpec := *spec.DeepCopy()
	newSpec.Sources = sources
	return newSpec
}
