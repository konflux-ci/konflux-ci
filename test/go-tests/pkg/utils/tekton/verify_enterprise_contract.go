package tekton

import app "github.com/konflux-ci/application-api/api/v1alpha1"

type VerifyEnterpriseContract struct {
	Snapshot            app.SnapshotSpec
	TaskBundle          string
	Name                string
	Namespace           string
	PolicyConfiguration string
	PublicKey           string
	Strict              bool
	EffectiveTime       string
	IgnoreRekor         bool
}

func (p *VerifyEnterpriseContract) WithComponentImage(imageRef string) {
	p.Snapshot.Components = []app.SnapshotComponent{
		{
			ContainerImage: imageRef,
		},
	}
}

func (p *VerifyEnterpriseContract) AppendComponentImage(imageRef string) {
	p.Snapshot.Components = append(p.Snapshot.Components, app.SnapshotComponent{
		ContainerImage: imageRef,
	})
}
