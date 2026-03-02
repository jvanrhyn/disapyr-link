# Kubernetes Deployment — disapyr-link

Target platform: Virtuozzo Kubernetes (standard K8s API).

---

## Prerequisites

- `kubectl` configured against your Virtuozzo cluster
- `cert-manager` installed with a `ClusterIssuer` named `letsencrypt-prod`
- Know your cluster's `storageClassName` and `ingressClassName`:

```bash
kubectl get storageclass
kubectl get ingressclass
```

Update `postgres-statefulset.yaml` (`storageClassName`) and `ingress.yaml` (`ingressClassName`) if the defaults don't match.

---

## Step 1 — Create the GHCR pull secret

```bash
kubectl create namespace disapyr

kubectl create secret docker-registry ghcr-pull-secret \
  --namespace disapyr \
  --docker-server=ghcr.io \
  --docker-username=jvanrhyn \
  --docker-password=<your-github-pat-with-read:packages>
```

Generate a GitHub PAT at: https://github.com/settings/tokens → scopes: `read:packages`

---

## Step 2 — Create the app secret

Copy `secret.yaml.example` to `secret.yaml` (never commit this file):

```bash
cp deploy/k8s/secret.yaml.example deploy/k8s/secret.yaml
```

Fill in base64-encoded values. To encode:

```bash
echo -n 'my-value' | base64
```

The `DATABASE_URL` must point to the in-cluster postgres service:
```
postgres://disapyr:<POSTGRES_PASSWORD>@postgres.disapyr.svc.cluster.local:5432/disapyr?sslmode=disable
```

Apply the secret:

```bash
kubectl apply -f deploy/k8s/secret.yaml
```

---

## Step 3 — Update domain

Edit `deploy/k8s/configmap.yaml` → set `BASE_URL` to your domain.
Edit `deploy/k8s/ingress.yaml` → set both `your-domain.example.com` entries.

---

## Step 4 — Deploy

```bash
# Apply everything except secret.yaml (already applied above)
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/postgres-statefulset.yaml

# Wait for postgres to be ready
kubectl rollout status statefulset/postgres -n disapyr

# Run migrations
kubectl apply -f deploy/k8s/migration-job.yaml
kubectl wait --for=condition=complete job/disapyr-migrate -n disapyr --timeout=120s

# Deploy app
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml
kubectl apply -f deploy/k8s/ingress.yaml
```

---

## Endpoints

| Path | Auth | Purpose |
|------|------|---------|
| `GET /healthz` | None | K8s liveness/readiness probe |
| `GET /health` | Basic Auth | Admin status page |
| `GET /health/logs` | Basic Auth | Admin log browser |

---

## CI/CD

`.github/workflows/build-push.yml` builds and pushes `ghcr.io/jvanrhyn/disapyr-link:latest` (and `:sha-<short>`) on every push to `main`.

To trigger a re-deploy after a new image is pushed:

```bash
kubectl rollout restart deployment/disapyr -n disapyr
```

---

## Rollback

```bash
kubectl rollout undo deployment/disapyr -n disapyr
```
