# 내부 기상정보 시스템

단일 Go 프로세스가 SQLite, 인증 수신 API, 읽기 API 및 React TV 화면을 동일 오리진으로 제공합니다. 런타임 외부 요청은 없으며 `/data`만 영구 쓰기 영역입니다. Go `1.24.6`, Node `22.18.0`, React `19.1.1`, Vite `7.1.1`, 순수 Go SQLite `modernc.org/sqlite 1.38.2`를 고정했습니다.

## 가정과 빠른 시작
실제 사업소/격자/특보 코드는 미확정이므로 `config/sites.yaml` 샘플을 운영 전 교체합니다. 알 수 없는 JSON 필드는 스키마 오타 방지를 위해 배치 전체를 거부합니다. 파일은 최대 20개이며 청크 업로드는 MVP 밖입니다.

```bash
cp .env.example .env
# INGEST_TOKEN을 32자 이상의 난수로 변경
make test && make build
docker compose --env-file .env up --build
open http://localhost:8080/display
```

구조: `backend`(Go), `frontend`(React), `migrations`(SQLite), `api/openapi.yaml`, `config/sites.yaml`, `scripts`, 배포/운영 문서. 화면은 Space 정지/재개, ←/→ 수동 이동 및 `?page=1|2&rotate=false`를 지원합니다.

## 기상청 단기예보 직접 연동

`KMA_SERVICE_KEY`를 설정하면 서버가 기상청 단기예보 조회서비스 2.0의 `getVilageFcst`를 사업소별 DFS 격자로 조회합니다. 수집 결과는 외부 정보시스템 배치와 동일한 내부 수신·검증·저장 경로를 사용합니다.

- `KMA_SERVICE_KEY`: 공공데이터포털 일반 인증키(미설정 시 직접 연동 비활성화)
- `KMA_ENDPOINT`: 기본값 `https://apis.data.go.kr/1360000/VilageFcstInfoService_2.0`
- `KMA_POLL_SECONDS`: 조회 주기, 기본값 `3600`
