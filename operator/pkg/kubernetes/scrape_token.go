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
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ScrapeTokenSecretName is the operand-namespace Secret holding the bound Prometheus scraper token.
	ScrapeTokenSecretName = "prometheus-scrape-token"
	// ScrapeTokenSecretKey is the data key used by ServiceMonitor bearerTokenSecret.
	//nolint:gosec // G101: secret data key name, not a credential
	ScrapeTokenSecretKey = "token"
	//nolint:gosec // G101: annotation key, not a credential
	scrapeTokenExpiresAtAnnotation = "konflux.konflux-ci.dev/scrape-token-expires-at"
)

// Default scrape-token timing. These values are shared by operand reconcilers,
// the leader-elected rotation broadcaster, and the operator scrape-token rotator.
//
// DefaultScrapeTokenTTL is the bound TokenRequest lifetime. One hour limits
// credential exposure if a token leaks while keeping rotation overhead modest.
// Kubernetes allows longer TTLs; shorter TTLs increase TokenRequest traffic.
//
// DefaultScrapeTokenRefreshRemaining is the fraction of TTL left at which a
// token is re-minted (0.5 → refresh at the halfway point, ~30 minutes with the
// default TTL). Refreshing before expiry gives Prometheus and the operand Secret
// time to pick up the new token without a scrape-auth gap at the boundary.
//
// DefaultScrapeTokenMinRequeue is the minimum wait before the next rotation
// check. It floors ScrapeTokenRequeueAfter when a token is still fresh (for
// example in unit tests and any adaptive scheduling callers).
//
// DefaultScrapeTokenRotationInterval is the fixed tick for TokenRotationBroadcaster
// and ScrapeTokenRotator. It is set below the ~30-minute refresh point (half of the
// default TTL) so controllers re-check token freshness periodically without a
// 1-minute poll that mostly no-ops. Operand Secret watch handles immediate
// recovery when prometheus-scrape-token is deleted; the broadcaster and operator
// rotator cover scheduled refresh and missed reconciles.
const (
	DefaultScrapeTokenTTL              = time.Hour
	DefaultScrapeTokenMinRequeue       = time.Minute
	DefaultScrapeTokenRotationInterval = 15 * time.Minute
	DefaultScrapeTokenRefreshRemaining = 0.5 // refresh when less than 50% of TTL remains
)

// TokenCreator mints bound service-account tokens for metrics scraping.
type TokenCreator interface {
	CreateScraperToken(
		ctx context.Context,
		scraper types.NamespacedName,
		ttl time.Duration,
	) (token string, expiresAt time.Time, err error)
}

// ClientTokenCreator uses the Kubernetes TokenRequest API.
type ClientTokenCreator struct {
	Clientset kubernetes.Interface
}

// NewClientTokenCreator builds a TokenCreator from a controller-runtime client config.
func NewClientTokenCreator(cfg *rest.Config) (*ClientTokenCreator, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &ClientTokenCreator{Clientset: cs}, nil
}

// CreateScraperToken implements TokenCreator.
func (c *ClientTokenCreator) CreateScraperToken(
	ctx context.Context,
	scraper types.NamespacedName,
	ttl time.Duration,
) (string, time.Time, error) {
	if c == nil || c.Clientset == nil {
		return "", time.Time{}, fmt.Errorf("token creator clientset is not configured")
	}
	seconds := int64(ttl.Seconds())
	if seconds < 600 {
		seconds = 600
	}
	tr, err := c.Clientset.CoreV1().ServiceAccounts(scraper.Namespace).CreateToken(
		ctx,
		scraper.Name,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				ExpirationSeconds: ptr.To(seconds),
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", time.Time{}, err
	}
	if tr.Status.Token == "" {
		return "", time.Time{}, fmt.Errorf("empty token for serviceaccount %s/%s", scraper.Namespace, scraper.Name)
	}
	expiresAt := tr.Status.ExpirationTimestamp.Time
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	return tr.Status.Token, expiresAt, nil
}

// ScrapeTokenApplyFunc persists the scrape token Secret (for example via tracking.Client.ApplyOwned).
type ScrapeTokenApplyFunc func(ctx context.Context, secret *corev1.Secret) error

// EnsureScrapeTokenInput configures EnsurePrometheusScrapeToken.
type EnsureScrapeTokenInput struct {
	Client           client.Reader
	Clock            clock.Clock
	TokenCreator     TokenCreator
	Scraper          types.NamespacedName
	OperandNamespace string
	Apply            ScrapeTokenApplyFunc
	TTL              time.Duration
}

// ScrapeTokenNeedsRefresh reports whether the Secret should be re-minted at now.
func ScrapeTokenNeedsRefresh(secret *corev1.Secret, now time.Time, ttl time.Duration) bool {
	if secret == nil || len(secret.Data[ScrapeTokenSecretKey]) == 0 {
		return true
	}
	expiresAt, ok := ScrapeTokenExpiry(secret)
	if !ok {
		return true
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return true
	}
	return remaining < time.Duration(float64(ttl)*DefaultScrapeTokenRefreshRemaining)
}

// ScrapeTokenExpiry parses the expiry annotation on a scrape token Secret.
func ScrapeTokenExpiry(secret *corev1.Secret) (time.Time, bool) {
	if secret == nil {
		return time.Time{}, false
	}
	raw := secret.Annotations[scrapeTokenExpiresAtAnnotation]
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ScrapeTokenRequeueAfter returns how long to wait before checking token freshness again.
func ScrapeTokenRequeueAfter(secret *corev1.Secret, now time.Time, ttl time.Duration) time.Duration {
	expiresAt, ok := ScrapeTokenExpiry(secret)
	if !ok {
		return DefaultScrapeTokenMinRequeue
	}
	refreshAt := expiresAt.Add(-time.Duration(float64(ttl) * DefaultScrapeTokenRefreshRemaining))
	wait := refreshAt.Sub(now)
	if wait < DefaultScrapeTokenMinRequeue {
		return DefaultScrapeTokenMinRequeue
	}
	return wait
}

// OperatorScrapeTokenFieldManager is the server-side apply field manager for operator scrape tokens.
const OperatorScrapeTokenFieldManager = "konflux-operator-scrape-token"

// ApplyScrapeTokenSecret persists a scrape token Secret without an owner reference.
func ApplyScrapeTokenSecret(ctx context.Context, c client.Client, secret *corev1.Secret) error {
	prepared := prepareSecretForApply(secret)
	return c.Patch(
		ctx,
		prepared,
		SSAPatch,
		client.FieldOwner(OperatorScrapeTokenFieldManager),
		client.ForceOwnership,
	)
}

// EnsurePrometheusScrapeToken creates or refreshes the operand scrape token Secret.
// It returns a suggested requeue interval for token rotation.
func EnsurePrometheusScrapeToken(ctx context.Context, in EnsureScrapeTokenInput) (time.Duration, error) {
	if in.Apply == nil {
		return 0, fmt.Errorf("scrape token apply func is required")
	}
	if in.TokenCreator == nil {
		return 0, fmt.Errorf("token creator is required")
	}
	if in.OperandNamespace == "" {
		return 0, fmt.Errorf("operand namespace is required")
	}
	if in.Scraper.Namespace == "" || in.Scraper.Name == "" {
		return 0, fmt.Errorf("scraper service account is required")
	}
	clk := in.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = DefaultScrapeTokenTTL
	}

	now := clk.Now()
	existing := &corev1.Secret{}
	err := in.Client.Get(ctx, types.NamespacedName{
		Name:      ScrapeTokenSecretName,
		Namespace: in.OperandNamespace,
	}, existing)
	if client.IgnoreNotFound(err) != nil {
		return 0, fmt.Errorf("get scrape token secret: %w", err)
	}
	if err == nil && !ScrapeTokenNeedsRefresh(existing, now, ttl) {
		if applyErr := in.Apply(ctx, prepareSecretForApply(existing)); applyErr != nil {
			return 0, fmt.Errorf("track scrape token secret: %w", applyErr)
		}
		return ScrapeTokenRequeueAfter(existing, now, ttl), nil
	}

	scraper := in.Scraper
	token, expiresAt, err := in.TokenCreator.CreateScraperToken(ctx, scraper, ttl)
	if err != nil {
		return 0, fmt.Errorf("create scraper token for %s/%s: %w", scraper.Namespace, scraper.Name, err)
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ScrapeTokenSecretName,
			Namespace: in.OperandNamespace,
			Annotations: map[string]string{
				scrapeTokenExpiresAtAnnotation: expiresAt.UTC().Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			ScrapeTokenSecretKey: []byte(token),
		},
	}
	if err := in.Apply(ctx, secret); err != nil {
		return 0, fmt.Errorf("apply scrape token secret: %w", err)
	}
	return ScrapeTokenRequeueAfter(secret, now, ttl), nil
}

// IsPrometheusScrapeTokenSecret reports whether obj is the operator-managed scrape token Secret.
func IsPrometheusScrapeTokenSecret(obj client.Object) bool {
	return obj != nil &&
		obj.GetName() == ScrapeTokenSecretName &&
		obj.GetNamespace() != ""
}

// GetPrometheusScrapeToken reads the token bytes from the operand scrape Secret.
func GetPrometheusScrapeToken(ctx context.Context, c client.Reader, namespace string) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: ScrapeTokenSecretName, Namespace: namespace}, secret); err != nil {
		return nil, err
	}
	token := secret.Data[ScrapeTokenSecretKey]
	if len(token) == 0 {
		return nil, fmt.Errorf(
			"scrape token secret %s/%s has empty %q",
			namespace, ScrapeTokenSecretName, ScrapeTokenSecretKey,
		)
	}
	return token, nil
}

func ensureSecretTypeMeta(secret *corev1.Secret) {
	if secret == nil {
		return
	}
	if secret.APIVersion == "" && secret.Kind == "" {
		secret.TypeMeta = metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		}
	}
}

// prepareSecretForApply returns a server-side-apply-safe copy of a live Secret.
func prepareSecretForApply(secret *corev1.Secret) *corev1.Secret {
	s := secret.DeepCopy()
	s.ResourceVersion = ""
	s.UID = ""
	s.Generation = 0
	s.CreationTimestamp = metav1.Time{}
	s.ManagedFields = nil
	s.OwnerReferences = nil
	ensureSecretTypeMeta(s)
	return s
}

// IgnoreScrapeTokenNotFound returns nil when the scrape token Secret is absent.
func IgnoreScrapeTokenNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
