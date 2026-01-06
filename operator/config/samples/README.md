# Sample Custom Resources

This directory contains sample YAML files for Konflux Custom Resources (CRs).

## Top-Level CR Sample

The `konflux_v1alpha1_konflux.yaml` sample is used in CI tests and represents a
functional example of the main Konflux CR that configures all components.

## Component CR Samples

All other sample files are provided to demonstrate the CRD structure and available
fields. These samples are **not** intended to represent meaningful functional examples,
but rather to showcase a complete-as-possible schema of each CRD type.

These samples are useful for:
- Understanding the available configuration options
- Validating CRD schema completeness (verified in unit tests)
