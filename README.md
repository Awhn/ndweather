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

## 기상정보 외부정보시스템 연동

`KMA_SERVICE_KEY`를 설정하면 서버가 외부정보시스템이 제공하는 기상청 단기예보 조회서비스 2.0 호환 엔드포인트의 `getVilageFcst`를 사업소별 DFS 격자로 조회합니다. 외부정보시스템은 별도의 배치 데이터 원천이 아니라 기상청 API 연계 구간이며, 운영 환경에서는 제공받은 엔드포인트로 `KMA_ENDPOINT`만 교체합니다. 기본값은 공공데이터포털 원본 엔드포인트이므로 로컬 검증에도 같은 요청·응답 규격을 사용합니다.

- `KMA_SERVICE_KEY`: 공공데이터포털 일반 인증키(미설정 시 직접 연동 비활성화)
- `KMA_ENDPOINT`: 외부정보시스템의 서비스 기본 URL 또는 `getVilageFcst` 전체 URL. 기본값 `https://apis.data.go.kr/1360000/VilageFcstInfoService_2.0`
- `KMA_POLL_SECONDS`: 조회 주기, 기본값 `3600`
- `KMA_APIHUB_KEY`: 레이더·태풍·특보 조회용 기상청 API허브 인증키(미설정 시 API허브 연동 비활성화)
- `KMA_APIHUB_ENDPOINT`: 기본값 `https://apihub.kma.go.kr`
- `KMA_APIHUB_POLL_SECONDS`: API허브 조회 주기, 기본값 `600`
