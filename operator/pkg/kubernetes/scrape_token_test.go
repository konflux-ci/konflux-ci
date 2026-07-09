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

package kubernetes

import (
	"context"
	"fmt"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeTokenCreator struct {
	token     string
	expiresAt time.Time
	calls     int
}

func (f *fakeTokenCreator) CreateScraperToken(
	_ context.Context,
	_ types.NamespacedName,
	_ time.Duration,
) (string, time.Time, error) {
	f.calls++
	return f.token, f.expiresAt, nil
}

func TestEnsurePrometheusScrapeTokenRequiresScraper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := EnsurePrometheusScrapeToken(ctx, EnsureScrapeTokenInput{
		Client:           &fakeSecretReader{},
		TokenCreator:     &fakeTokenCreator{token: "tok", expiresAt: time.Now().Add(time.Hour)},
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err == nil {
		t.Fatal("expected scraper validation error")
	}
}

func TestScrapeTokenNeedsRefresh(t *testing.T) {
	t.Parallel()
	ttl := time.Hour
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	fresh := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				scrapeTokenExpiresAtAnnotation: now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{ScrapeTokenSecretKey: []byte("tok")},
	}
	if ScrapeTokenNeedsRefresh(fresh, now, ttl) {
		t.Fatal("expected fresh token not to need refresh")
	}
	stale := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				scrapeTokenExpiresAtAnnotation: now.Add(20 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{ScrapeTokenSecretKey: []byte("tok")},
	}
	if !ScrapeTokenNeedsRefresh(stale, now, ttl) {
		t.Fatal("expected stale token to need refresh")
	}
}

func TestScrapeTokenRequeueAfter(t *testing.T) {
	t.Parallel()
	ttl := time.Hour
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				scrapeTokenExpiresAtAnnotation: now.Add(time.Hour).UTC().Format(time.RFC3339),
			},
		},
	}
	wait := ScrapeTokenRequeueAfter(secret, now, ttl)
	if wait != 30*time.Minute {
		t.Fatalf("expected 30m requeue, got %s", wait)
	}
}

func TestEnsurePrometheusScrapeTokenCreatesAndRefreshes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
	fake := &fakeTokenCreator{token: "first", expiresAt: clk.Now().Add(time.Hour)}

	var applied []*corev1.Secret
	scraper := types.NamespacedName{Namespace: "monitoring", Name: "prometheus"}
	in := EnsureScrapeTokenInput{
		Client:           &fakeSecretReader{},
		Clock:            clk,
		TokenCreator:     fake,
		Scraper:          scraper,
		OperandNamespace: "build-service",
		Apply: func(_ context.Context, secret *corev1.Secret) error {
			applied = append(applied, secret.DeepCopy())
			return nil
		},
		TTL: time.Hour,
	}

	result, err := EnsurePrometheusScrapeToken(ctx, in)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !result.TokenUpdated {
		t.Fatal("expected tokenUpdated on first mint")
	}
	if result.SecretExisted {
		t.Fatal("expected SecretExisted false on first mint")
	}
	if fake.calls != 1 {
		t.Fatalf("expected one token mint, got %d", fake.calls)
	}
	if len(applied) != 1 || string(applied[0].Data[ScrapeTokenSecretKey]) != "first" {
		t.Fatalf("unexpected applied secret: %#v", applied)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("unexpected requeue: %s", result.RequeueAfter)
	}

	// Second call with fresh secret in reader should not mint again.
	applied[0].ResourceVersion = "1"
	in.Client = &fakeSecretReader{secret: applied[0]}
	result, err = EnsurePrometheusScrapeToken(ctx, in)
	if err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	if result.TokenUpdated {
		t.Fatal("expected tokenUpdated false for fresh secret")
	}
	if !result.SecretExisted {
		t.Fatal("expected SecretExisted true for fresh secret")
	}
	if result.ResourceVersion != "1" {
		t.Fatalf("resourceVersion: got %q want %q", result.ResourceVersion, "1")
	}
	if fake.calls != 1 {
		t.Fatalf("expected no additional mint, got %d calls", fake.calls)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("unexpected second requeue: %s", result.RequeueAfter)
	}

	// Advance clock past refresh threshold and expect a new token.
	clk.SetTime(clk.Now().Add(40 * time.Minute))
	fake.token = "second"
	fake.expiresAt = clk.Now().Add(time.Hour)
	result, err = EnsurePrometheusScrapeToken(ctx, in)
	if err != nil {
		t.Fatalf("ensure refresh: %v", err)
	}
	if !result.TokenUpdated {
		t.Fatal("expected tokenUpdated on refresh")
	}
	if fake.calls != 2 {
		t.Fatalf("expected refresh mint, got %d calls", fake.calls)
	}
	if string(applied[len(applied)-1].Data[ScrapeTokenSecretKey]) != "second" {
		t.Fatalf("expected refreshed token")
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("unexpected refresh requeue: %s", result.RequeueAfter)
	}
}

type fakeSecretReader struct {
	secret *corev1.Secret
}

func (f *fakeSecretReader) Get(
	_ context.Context,
	key types.NamespacedName,
	obj client.Object,
	_ ...client.GetOption,
) error {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return fmt.Errorf("unexpected type %T", obj)
	}
	if f.secret == nil || key.Name != f.secret.Name || key.Namespace != f.secret.Namespace {
		return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
	}
	*secret = *f.secret
	return nil
}

func (f *fakeSecretReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("not implemented")
}

func TestScrapeTokenExpiry(t *testing.T) {
	t.Parallel()
	if _, ok := ScrapeTokenExpiry(nil); ok {
		t.Fatal("nil secret should not have expiry")
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
	if _, ok := ScrapeTokenExpiry(secret); ok {
		t.Fatal("missing annotation should not parse")
	}
	secret.Annotations[scrapeTokenExpiresAtAnnotation] = "not-a-timestamp"
	if _, ok := ScrapeTokenExpiry(secret); ok {
		t.Fatal("invalid annotation should not parse")
	}
}

func TestScrapeTokenNeedsRefresh_EdgeCases(t *testing.T) {
	t.Parallel()
	ttl := time.Hour
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if !ScrapeTokenNeedsRefresh(nil, now, ttl) {
		t.Fatal("nil secret should need refresh")
	}
	empty := &corev1.Secret{Data: map[string][]byte{}}
	if !ScrapeTokenNeedsRefresh(empty, now, ttl) {
		t.Fatal("empty token should need refresh")
	}
	expired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				scrapeTokenExpiresAtAnnotation: now.Add(-time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{ScrapeTokenSecretKey: []byte("tok")},
	}
	if !ScrapeTokenNeedsRefresh(expired, now, ttl) {
		t.Fatal("expired token should need refresh")
	}
}

func TestScrapeTokenRequeueAfter_MissingExpiry(t *testing.T) {
	t.Parallel()
	wait := ScrapeTokenRequeueAfter(&corev1.Secret{}, time.Now(), time.Hour)
	if wait != DefaultScrapeTokenMinRequeue {
		t.Fatalf("expected min requeue, got %s", wait)
	}
}

func TestIsPrometheusScrapeTokenSecret(t *testing.T) {
	t.Parallel()
	if IsPrometheusScrapeTokenSecret(nil) {
		t.Fatal("nil object should not match")
	}
	if IsPrometheusScrapeTokenSecret(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other"}}) {
		t.Fatal("wrong name should not match")
	}
	if !IsPrometheusScrapeTokenSecret(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ScrapeTokenSecretName, Namespace: "build-service"},
	}) {
		t.Fatal("expected scrape token secret to match")
	}
}

func TestIgnoreScrapeTokenNotFound(t *testing.T) {
	t.Parallel()
	if err := IgnoreScrapeTokenNotFound(nil); err != nil {
		t.Fatalf("nil error should stay nil: %v", err)
	}
	notFound := apierrors.NewNotFound(corev1.Resource("secrets"), ScrapeTokenSecretName)
	if err := IgnoreScrapeTokenNotFound(notFound); err != nil {
		t.Fatalf("not found should be ignored: %v", err)
	}
	other := fmt.Errorf("boom")
	if err := IgnoreScrapeTokenNotFound(other); err != other {
		t.Fatalf("expected original error, got %v", err)
	}
}

func TestGetPrometheusScrapeToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	ns := "build-service"

	t.Run("reads token bytes", func(t *testing.T) {
		t.Parallel()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ScrapeTokenSecretName, Namespace: ns},
			Data:       map[string][]byte{ScrapeTokenSecretKey: []byte("metrics-token")},
		}
		c := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		token, err := GetPrometheusScrapeToken(ctx, c, ns)
		if err != nil {
			t.Fatalf("get token: %v", err)
		}
		if string(token) != "metrics-token" {
			t.Fatalf("unexpected token: %q", token)
		}
	})

	t.Run("rejects empty token", func(t *testing.T) {
		t.Parallel()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ScrapeTokenSecretName, Namespace: ns},
			Data:       map[string][]byte{},
		}
		c := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		if _, err := GetPrometheusScrapeToken(ctx, c, ns); err == nil {
			t.Fatal("expected empty token error")
		}
	})
}

func TestApplyScrapeTokenSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := ctrlfake.NewClientBuilder().WithScheme(scheme).Build()
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ScrapeTokenSecretName,
			Namespace: "konflux-operator",
		},
		Data: map[string][]byte{ScrapeTokenSecretKey: []byte("applied")},
	}
	if err := ApplyScrapeTokenSecret(ctx, c, secret); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := &corev1.Secret{}
	secretNN := types.NamespacedName{Name: ScrapeTokenSecretName, Namespace: "konflux-operator"}
	if err := c.Get(ctx, secretNN, got); err != nil {
		t.Fatalf("get applied secret: %v", err)
	}
	if string(got.Data[ScrapeTokenSecretKey]) != "applied" {
		t.Fatalf("unexpected token bytes: %q", got.Data[ScrapeTokenSecretKey])
	}
}

func TestCreateScraperToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scraper := types.NamespacedName{Namespace: "monitoring", Name: "prometheus"}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	cs := kubefake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", func(action kubetesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != ScrapeTokenSecretKey {
			return false, nil, nil
		}
		return true, &authenticationv1.TokenRequest{
			Status: authenticationv1.TokenRequestStatus{
				Token:               "minted-token",
				ExpirationTimestamp: metav1.Time{Time: now.Add(time.Hour)},
			},
		}, nil
	})

	creator := &ClientTokenCreator{Clientset: cs}
	token, expiresAt, err := creator.CreateScraperToken(ctx, scraper, 30*time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if token != "minted-token" {
		t.Fatalf("unexpected token: %q", token)
	}
	if !expiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected expiry: %v", expiresAt)
	}

	var nilCreator *ClientTokenCreator
	if _, _, err := nilCreator.CreateScraperToken(ctx, scraper, time.Hour); err == nil {
		t.Fatal("expected nil creator error")
	}
}

func TestNewClientTokenCreator(t *testing.T) {
	t.Parallel()
	cfg := &rest.Config{Host: "https://127.0.0.1:6443"}
	creator, err := NewClientTokenCreator(cfg)
	if err != nil {
		t.Fatalf("new creator: %v", err)
	}
	if creator.Clientset == nil {
		t.Fatal("expected clientset")
	}
}

func TestEnsurePrometheusScrapeToken_Validation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := EnsurePrometheusScrapeToken(ctx, EnsureScrapeTokenInput{})
	if err == nil {
		t.Fatal("expected apply validation error")
	}
	_, err = EnsurePrometheusScrapeToken(ctx, EnsureScrapeTokenInput{
		Apply: func(context.Context, *corev1.Secret) error { return nil },
	})
	if err == nil {
		t.Fatal("expected token creator validation error")
	}
	_, err = EnsurePrometheusScrapeToken(ctx, EnsureScrapeTokenInput{
		TokenCreator: &fakeTokenCreator{},
		Apply:        func(context.Context, *corev1.Secret) error { return nil },
		Scraper:      types.NamespacedName{Namespace: "monitoring", Name: "prometheus"},
	})
	if err == nil {
		t.Fatal("expected operand namespace validation error")
	}
	_, err = EnsurePrometheusScrapeToken(ctx, EnsureScrapeTokenInput{
		TokenCreator:     &fakeTokenCreator{},
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
		OperandNamespace: "build-service",
	})
	if err == nil {
		t.Fatal("expected scraper validation error")
	}
}

func TestCreateScraperToken_ErrorPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scraper := types.NamespacedName{Namespace: "monitoring", Name: "prometheus"}

	cs := kubefake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", func(action kubetesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == ScrapeTokenSecretKey {
			return true, nil, fmt.Errorf("token api down")
		}
		return false, nil, nil
	})
	creator := &ClientTokenCreator{Clientset: cs}
	if _, _, err := creator.CreateScraperToken(ctx, scraper, time.Hour); err == nil {
		t.Fatal("expected token api error")
	}

	cs2 := kubefake.NewSimpleClientset()
	cs2.PrependReactor("create", "serviceaccounts", func(action kubetesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == ScrapeTokenSecretKey {
			return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{}}, nil
		}
		return false, nil, nil
	})
	creator2 := &ClientTokenCreator{Clientset: cs2}
	if _, _, err := creator2.CreateScraperToken(ctx, scraper, time.Hour); err == nil {
		t.Fatal("expected empty token error")
	}

	cs3 := kubefake.NewSimpleClientset()
	cs3.PrependReactor("create", "serviceaccounts", func(action kubetesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == ScrapeTokenSecretKey {
			return true, &authenticationv1.TokenRequest{
				Status: authenticationv1.TokenRequestStatus{Token: "tok"},
			}, nil
		}
		return false, nil, nil
	})
	creator3 := &ClientTokenCreator{Clientset: cs3}
	_, expiresAt, err := creator3.CreateScraperToken(ctx, scraper, 30*time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if expiresAt.IsZero() {
		t.Fatal("expected fallback expiry when apiserver omits timestamp")
	}

	creator4 := &ClientTokenCreator{Clientset: cs3}
	if _, _, err := (&ClientTokenCreator{Clientset: nil}).CreateScraperToken(ctx, scraper, time.Hour); err == nil {
		t.Fatal("expected nil clientset error")
	}
	_ = creator4
}

func TestEnsurePrometheusScrapeToken_GetAndApplyErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	_, err := EnsurePrometheusScrapeToken(ctx, EnsureScrapeTokenInput{
		Client:           &brokenSecretReader{err: fmt.Errorf("get failed")},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)},
		Scraper:          types.NamespacedName{Namespace: "monitoring", Name: "prometheus"},
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err == nil {
		t.Fatal("expected get error")
	}

	_, err = EnsurePrometheusScrapeToken(ctx, EnsureScrapeTokenInput{
		Client:           &fakeSecretReader{},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)},
		Scraper:          types.NamespacedName{Namespace: "monitoring", Name: "prometheus"},
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return fmt.Errorf("apply failed") },
	})
	if err == nil {
		t.Fatal("expected apply error")
	}
}

type brokenSecretReader struct {
	err error
}

func (b *brokenSecretReader) Get(context.Context, types.NamespacedName, client.Object, ...client.GetOption) error {
	return b.err
}

func (b *brokenSecretReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return b.err
}

func TestPrepareSecretForApplySetsTypeMeta(t *testing.T) {
	t.Parallel()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ScrapeTokenSecretName, Namespace: "build-service"},
		Data:       map[string][]byte{ScrapeTokenSecretKey: []byte("tok")},
	}
	prepared := prepareSecretForApply(secret)
	if prepared.APIVersion != "v1" || prepared.Kind != "Secret" {
		t.Fatalf("type meta: got APIVersion=%q Kind=%q", prepared.APIVersion, prepared.Kind)
	}
	if prepared.ResourceVersion != "" {
		t.Fatal("expected resourceVersion to be cleared")
	}
}

func TestGetPrometheusScrapeToken_EmptyData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ScrapeTokenSecretName, Namespace: "build-service"},
		Data:       map[string][]byte{},
	}
	c := ctrlfake.NewClientBuilder().WithObjects(secret).Build()
	_, err := GetPrometheusScrapeToken(ctx, c, "build-service")
	if err == nil {
		t.Fatal("expected error for empty token data")
	}
}
