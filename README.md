# sample-msa — HelloCalc MSA

A tiny two-service reference workload: a calculator split into a **frontend** (UI + API gateway) and a **backend** (calculator API), each a single static Go binary.

## Team Daisy 연동 정보

이 레포는 Daisy 배포 시스템이 배포할 **"사용자의 앱 저장소"를 흉내 낸 서비스 2개짜리 MSA 샘플**이에요. 샘플 앱의 이름은 **HelloCalc**이고, [`sample-monolith`](https://github.com/Softbank-Hackathon-2026-Team-Daisy/sample-monolith)의 같은 앱을 두 서비스로 나눈 버전이에요. 앱 동작은 일부러 단순하게 고정해 두었으니, 배포 시스템을 검증할 때 기준 앱으로 쓰면 돼요. 공통 맥락과 스키마 원본은 [`daisy/CLAUDE.md`](https://github.com/Softbank-Hackathon-2026-Team-Daisy/daisy/blob/main/CLAUDE.md)에 있어요.

```
사용자 ──► hellocalc-frontend (공개, :8080) ──/api/*──► hellocalc-backend (내부 전용, :8080)
             UI · /health · /readyz · /version        계산 API · /health · /readyz · /version
             BACKEND_URL=http://<backend 서비스 이름>:8080
```

### 앱 연결 요구사항 체크

| 요구사항 | 위치 | 내용 |
|---|---|---|
| 서비스별 `Dockerfile` | [`services/backend/Dockerfile`](services/backend/Dockerfile), [`services/frontend/Dockerfile`](services/frontend/Dockerfile) | 멀티스테이지 빌드, distroless 비루트(UID 65532), 셸 없음, amd64·arm64, 포트 8080, `HEALTHCHECK` 내장. **빌드 컨텍스트는 레포 루트**예요 (`docker build -f services/<서비스>/Dockerfile .`) |
| 서비스별 `deploy.yaml` | [`services/backend/deploy.yaml`](services/backend/deploy.yaml), [`services/frontend/deploy.yaml`](services/frontend/deploy.yaml) | 팀 스키마(§5, 9/29 초안) 그대로 |
| 서비스 간 통신 (서비스 이름으로 연결) | frontend 환경변수 `BACKEND_URL` | frontend가 `/api/*`를 `BACKEND_URL`로 넘겨요. Compose에서는 `http://backend:8080`, Kubernetes에서는 `http://hellocalc-backend`를 써요 |
| Compose 파일 | [`compose.yaml`](compose.yaml) | `docker compose up --build` 한 번으로 두 서비스가 떠요. backend는 호스트에 공개하지 않아요 |
| `/health` 엔드포인트 | 두 서비스 모두 `GET /health` | `200 {"status":"ok"}`. `/healthz`도 같은 응답 |
| `.github/workflows/` | [`.github/workflows/ci.yml`](.github/workflows/ci.yml) | N-01 이미지 파이프라인 (서비스 2개) |

### 서비스별 `deploy.yaml`

| 필드 | `hellocalc-backend` | `hellocalc-frontend` |
|---|---|---|
| `port` | 8080 | 8080 |
| `healthcheck` | `/health` | `/health` (backend 상태와 무관) |
| `env` | `LOG_LEVEL`, `SHUTDOWN_TIMEOUT` (선택) | **`BACKEND_URL` (필수)**, `LOG_LEVEL`, `SHUTDOWN_TIMEOUT` |
| `secrets` / `database` | 없음 / `false` | 없음 / `false` |
| 공개 여부 (주석) | 내부 전용 | 외부 공개 |

**팀 스키마로 아직 표현할 수 없는 것 (논의 필요):** 현재 스키마는 서비스 하나만 기준이라, 아래 내용은 `deploy.yaml`에 **주석으로만** 적어 두었어요. 스키마는 파트 사이의 약속이라 임의로 필드를 추가하지 않았어요.

- **서비스 목록:** 한 레포에 서비스가 여러 개예요. 지금은 서비스 폴더마다 `deploy.yaml`을 하나씩 둬요.
- **의존성과 배포 순서:** backend를 먼저 배포하고, 그 주소를 frontend의 `BACKEND_URL`에 넣어야 해요.
- **공개 여부:** frontend만 외부에 공개하고, backend는 내부 전용이에요.

### 서비스 간 통신 계약

- frontend는 `BACKEND_URL`이 없거나 잘못되면 **exit 1로 바로 종료**해요. 설정 전달 실패가 즉시 드러나요.
- `BACKEND_URL`에는 경로 접두사도 넣을 수 있어요 (예: `https://api.example.com/calc`).
- frontend의 **`/readyz`는 backend의 `/readyz`까지 확인**해요. 서비스 간 연결이 동작할 때만 트래픽을 받아요. backend에 닿지 않으면 `503 {"status":"not ready","reason":"backend unreachable"}`를 반환해요.
- **`/health`(liveness)는 backend와 무관**해요. backend 장애로 frontend가 재시작되는 연쇄 장애가 생기지 않아요.
- backend가 죽으면 `/api/*`는 `502 {"error":"backend unavailable"}`를 반환하고, 화면에도 그렇게 표시돼요.
- `X-Request-ID`는 frontend에서 backend까지 그대로 넘어가요. **같은 ID가 두 서비스 로그에 모두 찍혀서** 요청을 끝까지 추적할 수 있어요. 로그마다 `service` 필드가 있어서 어느 서비스 로그인지 구분돼요.
- frontend의 `/version`은 자기 정보에 backend의 `/version`을 `backend` 필드로 붙여서 돌려줘요. **요청 한 번으로 두 서비스의 배포 산출물(커밋)을 확인**할 수 있어요.

### 환경별 `BACKEND_URL` 예시

| 대상 환경 | 값 |
|---|---|
| Docker Compose / 온프레미스 Docker 네트워크 | `http://backend:8080` (서비스·컨테이너 이름) |
| Kubernetes | `http://hellocalc-backend` (Service 이름, 포트 80) |
| AWS ECS | Service Connect 또는 Cloud Map 이름 (예: `http://hellocalc-backend:8080`) |
| GCP Cloud Run | backend 서비스 URL (예: `https://hellocalc-backend-xxxx.a.run.app`). 아래 주의사항 참고 |

**Cloud Run 주의사항:** Cloud Run은 기본적으로 IAM 인증된 호출만 받아요. 그런데 frontend 프록시는 Google ID 토큰을 붙이지 않아서, 그대로 배포하면 backend 호출이 403으로 막혀요.

- backend는 **인증 없는 호출을 허용**해서 배포해요 (`--allow-unauthenticated`, Terraform이라면 `roles/run.invoker`를 `allUsers`에 부여).
- 외부 노출은 IAM 대신 네트워크로 막아요. backend의 ingress를 `internal`로 두고, frontend에는 Direct VPC egress(모든 트래픽을 VPC로)를 설정하고, 그 서브넷에 Private Google Access를 켜요.
- ingress를 `internal`로 두지 않으면 backend URL을 아는 누구나 계산 API를 직접 호출할 수 있어요. 데모용 샘플이라 데이터 위험은 없지만, "backend는 내부 전용"이라는 설계와는 달라져요.

### N-01 GitHub Actions 파이프라인 (`ci.yml`)

```
PR        : test(make check) → e2e(Compose로 두 서비스 기동 → 스모크 테스트 → 정상 종료 확인)
main push : 위 과정 → backend·frontend 이미지(amd64·arm64) → ghcr.io 푸시 → 배포 서비스에 이벤트 전달
```

| 항목 | 값 |
|---|---|
| 이미지 | `ghcr.io/softbank-hackathon-2026-team-daisy/sample-msa-backend:<커밋 해시 40자>`<br>`ghcr.io/softbank-hackathon-2026-team-daisy/sample-msa-frontend:<커밋 해시 40자>` |
| 태그 규칙 | 항상 커밋 해시(`github.sha`). `latest`는 쓰지 않아요 |
| 레지스트리 | GHCR (`GITHUB_TOKEN` 사용, 별도 비밀값 없음). 팀 레지스트리가 정해지면 `images` 잡만 바꾸면 돼요 |
| 이벤트 전달 | 저장소 Secret `DAISY_WEBHOOK_URL`이 있으면 아래 JSON을 POST해요. 없으면 notice만 남기고 건너뛰어요 |

```json
{
  "repository": "Softbank-Hackathon-2026-Team-Daisy/sample-msa",
  "commit": "<40자 커밋 해시>",
  "ref": "refs/heads/main",
  "images": {
    "backend": "ghcr.io/softbank-hackathon-2026-team-daisy/sample-msa-backend:<커밋 해시>",
    "frontend": "ghcr.io/softbank-hackathon-2026-team-daisy/sample-msa-frontend:<커밋 해시>"
  },
  "run_url": "https://github.com/.../actions/runs/<id>"
}
```

- 모놀리스는 이미지가 하나라 `"image"` 필드를 쓰고, MSA는 서비스별 `"images"` 필드를 써요. 웹훅 수신 API가 정해지면 `ci.yml`의 `notify` 잡에서 payload를 맞춰 주세요.
- GHCR 패키지는 처음 푸시할 때 비공개로 만들어질 수 있어요. 인증 없이 이미지를 받아야 하는 환경이라면 패키지 설정에서 공개로 바꾸거나, 그 환경에 레지스트리 인증을 넣어 주세요.

### 배포 검증 방법

배포한 뒤에는 **공개된 frontend 주소 하나**로 스모크 테스트를 돌려요. `sh`와 `curl`만 있으면 되고, frontend → backend 경로까지 함께 검사해요. 모두 통과하면 exit 0, 하나라도 실패하면 exit 1이에요.

```sh
BASE_URL=https://<frontend 주소> EXPECTED_COMMIT=<배포한 커밋 해시> ./scripts/smoke-test.sh
```

| 확인 대상 | 방법 |
|---|---|
| 두 서비스가 떠 있고 서로 연결됐는지 | frontend `GET /readyz` → 200 (backend readiness 포함) |
| 의도한 이미지가 두 서비스 모두에 배포됐는지 | frontend `GET /version`의 `commit`과 `backend.commit`. 스모크 테스트에서는 `EXPECTED_COMMIT`로 확인 |
| 서비스 간 통신 | `POST /api/calculate` (frontend → backend) |
| 요청 추적 | 응답 `X-Request-ID`와 같은 ID가 두 서비스 로그에 모두 있는지 |
| 장애 격리 | backend를 내리면 frontend `/readyz` 503, `/health` 200, API 502 |
| 종료 처리 | SIGTERM → 새 연결 거부 → 처리 중 요청 완료 → exit 0 (`docker compose stop`으로 확인) |
| 리소스 | 서비스당 유휴 메모리 약 10 MiB |

backend가 외부에서 보이는 환경이라면 `BACKEND_BASE_URL=<backend 주소>`를 추가해 backend도 직접 검사할 수 있어요.

**알려진 한계**

- 종료 전 대기 설정(preStop 등)이 없어요. 그래서 로드밸런서나 Kubernetes가 대상에서 빼기 전 몇 초 동안 들어온 요청은 연결 거부될 수 있어요.
- frontend의 readiness가 backend에 묶여 있어요. backend 전체가 내려가면 frontend도 트래픽에서 빠져요. 서비스 간 연결 검증을 위한 의도된 동작이에요.

### 협업 규칙 (daisy `CONTRIBUTING.md` 요약)

- `main`에 직접 push하지 않고 PR로 올려요. 리뷰 1명 승인 후 squash merge해요.
- 브랜치는 `{파트}/{타입}-{설명}` 형식이에요 (예: `infra/feat-msa-compose`). 커밋은 `feat(infra): ...`처럼 적어요.
- `.env`, 키 파일 같은 비밀값은 커밋하지 않아요. 필요한 값은 GitHub Actions Secrets에 넣어요.
- 담당: 황지환 `[미정]`

## Reference

### Layout

```
internal/app/          shared entrypoint: config, signals, `healthcheck` / `version` subcommands
internal/httpx/        request ID, JSON logging, recovery, security headers, probes, graceful serve
internal/config/       HOST / PORT / LOG_LEVEL / SHUTDOWN_TIMEOUT
internal/buildinfo/    version / commit / build time (set via -ldflags)
internal/calculator/   arithmetic (+ - * / % ^), no eval
services/backend/      calculator API service     + Dockerfile + deploy.yaml
services/frontend/     UI + /api/* reverse proxy  + Dockerfile + deploy.yaml (web/ embedded)
compose.yaml  deploy/kubernetes.yaml  scripts/smoke-test.sh  .github/workflows/ci.yml
```

### Run and test

```sh
make run      # backend on :8081, frontend on :8080 (BACKEND_URL=http://localhost:8081)
make check    # gofmt check, go vet, go test, build bin/backend and bin/frontend
make up       # docker compose up --build --wait  →  http://localhost:8080
make smoke    # smoke test against BASE_URL (default http://localhost:8080)
make down
```

If port 8080 is taken, use `FRONTEND_PORT=18080 docker compose up --build`.

### Configuration

| Variable           | Service  | Default   | Description                                             |
|--------------------|----------|-----------|---------------------------------------------------------|
| `HOST`             | both     | `0.0.0.0` | Listen address                                          |
| `PORT`             | both     | `8080`    | Listen port                                             |
| `LOG_LEVEL`        | both     | `info`    | `debug`, `info`, `warn`, `error`                        |
| `SHUTDOWN_TIMEOUT` | both     | `15s`     | Maximum time allowed for in-flight requests on shutdown |
| `BACKEND_URL`      | frontend | required  | Absolute http(s) URL of the backend service             |

Invalid values make the process exit with status 1 and a message on stderr.

### Endpoints

| Endpoint                   | Backend                           | Frontend                                         |
|----------------------------|-----------------------------------|--------------------------------------------------|
| `GET /`                    | 404 (no UI)                       | Calculator UI                                    |
| `GET /health`, `/healthz`  | `200 {"status":"ok"}`             | `200 {"status":"ok"}`                            |
| `GET /readyz`              | `200`; `503` while shutting down  | `200` only if backend `/readyz` is 200           |
| `GET /version`             | build metadata                    | build metadata + `"backend": {...}` or `null`    |
| `/api/*`                   | `POST /api/calculate`             | forwarded to `BACKEND_URL`; `502` if unreachable |

`POST /api/calculate {"left":12.5,"operator":"*","right":4}` returns `{"result":50}`. Operators: `+ - * / % ^`.

| Status | Cause |
|--------|-------|
| `400`  | Malformed JSON, unknown or missing fields, unsupported operator |
| `413`  | Body larger than 4 KiB |
| `415`  | `Content-Type` is not `application/json` |
| `422`  | Division by zero, result out of range, non-real result |

**Logging.** Each request produces one JSON log line on stdout with these fields: `timestamp`, `level`, `msg`, `service`, `method`, `path`, `status`, `duration_ms`, `bytes`, `remote_addr`, `request_id`. Bodies and query strings are never logged. Probe requests are logged at `debug` level.

**Shutdown.** On `SIGTERM` or `SIGINT` each service marks itself not ready, stops accepting connections, waits up to `SHUTDOWN_TIMEOUT` for in-flight requests, then exits with status 0.

### Kubernetes

[`deploy/kubernetes.yaml`](deploy/kubernetes.yaml) is vendor-neutral. For each service it defines a `Deployment` (2 replicas, rolling update with `maxUnavailable: 0`) and a `ClusterIP` `Service`. Both services use the same settings:

- probes: readiness on `/readyz`, liveness on `/health`
- resources: requests 10m CPU / 16Mi memory, limits 250m CPU / 64Mi memory
- security: non-root user, read-only root filesystem, all capabilities dropped

The frontend reaches the backend through `BACKEND_URL=http://hellocalc-backend`.

```sh
kubectl apply -f deploy/kubernetes.yaml
kubectl set image deployment/hellocalc-backend hellocalc-backend=<registry>/sample-msa-backend:<sha>
kubectl set image deployment/hellocalc-frontend hellocalc-frontend=<registry>/sample-msa-frontend:<sha>
kubectl port-forward service/hellocalc-frontend 8080:80
```

## License

[MIT](LICENSE)
