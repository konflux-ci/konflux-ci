/*
Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package serviceaccount

import (
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func TestMergeSecretRefsIntoServiceAccount(t *testing.T) {
	const reg = "regcred-internal-registry"
	otherPull := corev1.LocalObjectReference{Name: "other-pull"}
	otherSec := corev1.ObjectReference{Name: "other-docker"}

	t.Run("adds cred to both lists when SA lists are empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{}
		g.Expect(MergeSecretRefsIntoServiceAccount(sa, reg)).To(gomega.BeTrue())
		g.Expect(sa.ImagePullSecrets).To(gomega.Equal([]corev1.LocalObjectReference{{Name: reg}}))
		g.Expect(sa.Secrets).To(gomega.Equal([]corev1.ObjectReference{{Name: reg}}))
	})

	t.Run("is a no-op when cred is already on both lists", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: reg}},
			Secrets:          []corev1.ObjectReference{{Name: reg}},
		}
		g.Expect(MergeSecretRefsIntoServiceAccount(sa, reg)).To(gomega.BeFalse())
		g.Expect(sa.ImagePullSecrets).To(gomega.Equal([]corev1.LocalObjectReference{{Name: reg}}))
		g.Expect(sa.Secrets).To(gomega.Equal([]corev1.ObjectReference{{Name: reg}}))
	})

	t.Run("adds secrets entry when only imagePullSecrets lists the cred", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: reg}},
			Secrets:          []corev1.ObjectReference{otherSec},
		}
		g.Expect(MergeSecretRefsIntoServiceAccount(sa, reg)).To(gomega.BeTrue())
		g.Expect(sa.ImagePullSecrets).To(gomega.Equal([]corev1.LocalObjectReference{{Name: reg}}))
		g.Expect(sa.Secrets).To(gomega.Equal([]corev1.ObjectReference{otherSec, {Name: reg}}))
	})

	t.Run("adds imagePullSecrets entry when only secrets lists the cred", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: []corev1.LocalObjectReference{otherPull},
			Secrets:          []corev1.ObjectReference{{Name: reg}},
		}
		g.Expect(MergeSecretRefsIntoServiceAccount(sa, reg)).To(gomega.BeTrue())
		g.Expect(sa.ImagePullSecrets).To(gomega.Equal([]corev1.LocalObjectReference{otherPull, {Name: reg}}))
		g.Expect(sa.Secrets).To(gomega.Equal([]corev1.ObjectReference{{Name: reg}}))
	})

	t.Run("preserves unrelated refs when adding cred", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: []corev1.LocalObjectReference{otherPull},
			Secrets:          []corev1.ObjectReference{otherSec},
		}
		g.Expect(MergeSecretRefsIntoServiceAccount(sa, reg)).To(gomega.BeTrue())
		g.Expect(sa.ImagePullSecrets).To(gomega.Equal([]corev1.LocalObjectReference{otherPull, {Name: reg}}))
		g.Expect(sa.Secrets).To(gomega.Equal([]corev1.ObjectReference{otherSec, {Name: reg}}))
	})
}

func TestStripSecretRefsFromServiceAccount(t *testing.T) {
	const reg = "regcred-internal-registry"
	otherPull := corev1.LocalObjectReference{Name: "other-pull"}
	otherSec := corev1.ObjectReference{Name: "other-docker"}

	t.Run("removes cred from both lists and keeps other refs", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: reg}, otherPull},
			Secrets:          []corev1.ObjectReference{{Name: reg}, otherSec},
		}
		g.Expect(StripSecretRefsFromServiceAccount(sa, reg)).To(gomega.BeTrue())
		g.Expect(sa.ImagePullSecrets).To(gomega.Equal([]corev1.LocalObjectReference{otherPull}))
		g.Expect(sa.Secrets).To(gomega.Equal([]corev1.ObjectReference{otherSec}))
	})

	t.Run("is a no-op when cred is not listed", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: []corev1.LocalObjectReference{otherPull},
			Secrets:          []corev1.ObjectReference{otherSec},
		}
		g.Expect(StripSecretRefsFromServiceAccount(sa, reg)).To(gomega.BeFalse())
		g.Expect(sa.ImagePullSecrets).To(gomega.Equal([]corev1.LocalObjectReference{otherPull}))
		g.Expect(sa.Secrets).To(gomega.Equal([]corev1.ObjectReference{otherSec}))
	})

	t.Run("clears imagePullSecrets when only that list had the cred", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: reg}},
			Secrets:          nil,
		}
		g.Expect(StripSecretRefsFromServiceAccount(sa, reg)).To(gomega.BeTrue())
		g.Expect(sa.ImagePullSecrets).To(gomega.BeNil())
		g.Expect(sa.Secrets).To(gomega.BeNil())
	})

	t.Run("clears secrets when only that list had the cred", func(t *testing.T) {
		g := gomega.NewWithT(t)
		sa := &corev1.ServiceAccount{
			ImagePullSecrets: nil,
			Secrets:          []corev1.ObjectReference{{Name: reg}},
		}
		g.Expect(StripSecretRefsFromServiceAccount(sa, reg)).To(gomega.BeTrue())
		g.Expect(sa.ImagePullSecrets).To(gomega.BeNil())
		g.Expect(sa.Secrets).To(gomega.BeNil())
	})
}

func TestFilterLocalObjectRefs(t *testing.T) {
	g := gomega.NewWithT(t)
	in := []corev1.LocalObjectReference{{Name: "a"}, {Name: "drop"}, {Name: "b"}}
	out, changed := FilterLocalObjectRefs(in, "drop")
	g.Expect(changed).To(gomega.BeTrue())
	g.Expect(out).To(gomega.Equal([]corev1.LocalObjectReference{{Name: "a"}, {Name: "b"}}))

	out2, changed2 := FilterLocalObjectRefs(out, "missing")
	g.Expect(changed2).To(gomega.BeFalse())
	g.Expect(out2).To(gomega.Equal(out))
}

func TestFilterObjectRefs(t *testing.T) {
	g := gomega.NewWithT(t)
	in := []corev1.ObjectReference{{Name: "a"}, {Name: "drop"}, {Name: "b"}}
	out, changed := FilterObjectRefs(in, "drop")
	g.Expect(changed).To(gomega.BeTrue())
	g.Expect(out).To(gomega.Equal([]corev1.ObjectReference{{Name: "a"}, {Name: "b"}}))

	out2, changed2 := FilterObjectRefs(out, "missing")
	g.Expect(changed2).To(gomega.BeFalse())
	g.Expect(out2).To(gomega.Equal(out))
}
