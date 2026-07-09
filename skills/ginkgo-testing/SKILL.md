---
name: ginkgo-testing
description: Ginkgo/Gomega testing patterns for Konflux — test cleanup (envtest), Eventually/Consistently soft assertions, and helper conventions. Use when writing or reviewing Ginkgo tests.
---

# Ginkgo / Gomega Testing Patterns

## Test cleanup in envtest

envtest has no garbage collector — deleted parent CRs do not cascade-delete their children. Two cleanup patterns handle this:

- **`DeferCleanupParentAndChildren`** — for tests involving **cluster-scoped** children (ClusterRole, ClusterRoleBinding, VWC, MWC, ConsoleLink, SCC). Deletes parent first (stopping reconciles), then explicitly deletes orphaned children. **Never** register separate `DeferCleanup` calls for parent and cluster-scoped children — Ginkgo's LIFO ordering will delete children first while the reconciler is still active, causing flaky timeouts.
- **`DeferCleanup(testutil.DeleteAndWait, ...)`** — for tests that only need to clean up the parent CR (no cluster-scoped children). Stale namespaced children are harmless: the next test's reconcile updates their ownerReferences to the new parent via `SetControllerReference` in `ApplyOwned`.

```go
// Cluster-scoped children: use DeferCleanupParentAndChildren
Expect(k8sClient.Create(ctx, parentCR)).To(Succeed())
testutil.DeferCleanupParentAndChildren(k8sClient, parentCR,
    &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "child-role"}},
    &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "child-binding"}},
)

// Namespaced-only children: simple DeferCleanup is sufficient
Expect(k8sClient.Create(ctx, parentCR)).To(Succeed())
DeferCleanup(testutil.DeleteAndWait, k8sClient, parentCR)
```

## Eventually / Consistently and soft assertions

Inside `Eventually` or `Consistently` callbacks, **never** use global `Expect()` — it triggers a hard Ginkgo panic that aborts the callback immediately, preventing `Eventually` from retrying. Instead, use the `func(g Gomega)` callback signature and call `g.Expect()` for soft failures that let `Eventually` retry on the next poll.

Any helper function called inside an `Eventually` block must accept a `Gomega` parameter and use `g.Expect()` internally. See `echoGetG` in `test/go-tests/proxy_test.go` for a real example.

```go
// ✗ Wrong — global Expect() panics on failure, Eventually cannot retry
Eventually(func() {
    resp := echoGet(url)
    Expect(resp.StatusCode).To(Equal(200))
}).Should(Succeed())

// ✓ Correct — g.Expect() signals failure softly, Eventually retries
Eventually(func(g Gomega) {
    resp := echoGetG(g, url)
    g.Expect(resp.StatusCode).To(Equal(200))
}).Should(Succeed())
```

## Kubernetes API error assertions

When asserting that a resource does not exist, use `apierrors.IsNotFound()` — never match error strings:

```go
// ✗ Wrong — brittle, breaks across K8s versions
g.Expect(k8sClient.Get(ctx, nn, obj)).To(MatchError(ContainSubstring("not found")))

// ✓ Correct — uses typed status code
err := k8sClient.Get(ctx, nn, obj)
g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
```

Import via `apierrors "k8s.io/apimachinery/pkg/api/errors"` (some files import the package without an alias and call `errors.IsNotFound()` — both work, but `apierrors` avoids confusion with the standard `errors` package).

The same principle applies to other typed API error checks — prefer `apierrors.IsAlreadyExists()`, `apierrors.IsConflict()`, etc. over string matching.
