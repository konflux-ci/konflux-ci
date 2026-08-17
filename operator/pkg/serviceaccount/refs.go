/*
Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package serviceaccount holds small helpers for mutating corev1.ServiceAccount credential lists.
package serviceaccount

import (
	corev1 "k8s.io/api/core/v1"
)

// MergeSecretRefsIntoServiceAccount appends secretName to imagePullSecrets and secrets when not
// already listed. Returns true if either slice was modified.
func MergeSecretRefsIntoServiceAccount(sa *corev1.ServiceAccount, secretName string) bool {
	changed := false
	if !localObjectRefContainsName(sa.ImagePullSecrets, secretName) {
		sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{Name: secretName})
		changed = true
	}
	if !objectRefContainsName(sa.Secrets, secretName) {
		sa.Secrets = append(sa.Secrets, corev1.ObjectReference{Name: secretName})
		changed = true
	}
	return changed
}

// StripSecretRefsFromServiceAccount removes secretName from imagePullSecrets and secrets. Returns
// true if either slice changed.
func StripSecretRefsFromServiceAccount(sa *corev1.ServiceAccount, secretName string) bool {
	changed := false
	if out, ch := FilterLocalObjectRefs(sa.ImagePullSecrets, secretName); ch {
		sa.ImagePullSecrets = out
		changed = true
	}
	if out, ch := FilterObjectRefs(sa.Secrets, secretName); ch {
		sa.Secrets = out
		changed = true
	}
	return changed
}

// FilterLocalObjectRefs returns a copy of refs without entries whose Name equals drop. The second
// return value is true if any element was removed.
func FilterLocalObjectRefs(refs []corev1.LocalObjectReference, drop string) ([]corev1.LocalObjectReference, bool) {
	var out []corev1.LocalObjectReference
	changed := false
	for _, r := range refs {
		if r.Name == drop {
			changed = true
			continue
		}
		out = append(out, r)
	}
	return out, changed
}

// FilterObjectRefs returns a copy of refs without entries whose Name equals drop. The second return
// value is true if any element was removed.
func FilterObjectRefs(refs []corev1.ObjectReference, drop string) ([]corev1.ObjectReference, bool) {
	var out []corev1.ObjectReference
	changed := false
	for _, r := range refs {
		if r.Name == drop {
			changed = true
			continue
		}
		out = append(out, r)
	}
	return out, changed
}

func localObjectRefContainsName(refs []corev1.LocalObjectReference, name string) bool {
	for _, r := range refs {
		if r.Name == name {
			return true
		}
	}
	return false
}

func objectRefContainsName(refs []corev1.ObjectReference, name string) bool {
	for _, r := range refs {
		if r.Name == name {
			return true
		}
	}
	return false
}
