# Railway 검증 배포
1. GitHub 저장소를 Railway 프로젝트의 서비스 한 개에 연결합니다.
2. 빌더가 루트 `Dockerfile`을 감지하고 이미지 빌드 로그가 성공하는지 확인합니다.
3. Volume 하나를 만들고 **mount path `/data`** 로 서비스에 연결합니다.
4. Variables의 **Suggested Variables**에서 루트 `.env.example`을 가져옵니다. 필수값은 `INGEST_MODE=http`, 32자 이상 난수 `INGEST_TOKEN`, `DEMO_MODE=true`(검증 시), `DATA_DIR=/data`, `SQLITE_PATH=/data/weather.db`, `ASSET_DIR=/data/assets`, `INBOX_DIR=/data/inbox`입니다. **사업소 예보를 표시하려면 `KMA_SERVICE_KEY`에 공공데이터포털 단기예보 조회서비스 2.0의 Decoding 인증키를 입력해야 합니다.** `KMA_APIHUB_KEY`는 레이더·태풍·특보 전용이므로 이것만 설정해서는 예보가 수집되지 않습니다. `KMA_API_KEY`도 예보 키의 별칭으로 사용할 수 있습니다. 외부정보시스템을 경유하면 각 Endpoint 변수를 제공받은 호환 주소로 교체합니다. `PORT`는 Railway 주입값을 그대로 둡니다. 변수 변경 후 새 배포가 완료되어 프로세스가 재시작됐는지 확인합니다.
5. Healthcheck path를 `/health/ready`로, timeout을 30초 이상으로 지정합니다.
6. 검증용 Public Domain을 생성합니다. 운영 업무망에는 공개 도메인을 사용하지 않습니다.
7. `BASE_URL=https://... INGEST_TOKEN='...' ./scripts/send-demo-batch.sh`로 전송하고 HTTP 200을 확인합니다.
8. `https://.../display`에서 두 화면, DEMO DATA, 5초 회전을 확인합니다. `READ_API_TOKEN`을 설정했다면 `https://.../display#token=<READ_API_TOKEN>`으로 엽니다.
9. `/api/v1/status`에서 `integrations.forecastEnabled:true`와 `collectors.kma_forecast.lastSuccessfulAt`을 확인합니다. `lastAttemptFailed:true`이면 Railway 로그에서 `KMA collection failed`를 검색합니다.
10. redeploy 후 `/api/v1/observations/latest`와 `/data/assets` 메타데이터가 유지되는지 확인합니다.
11. Volume 없이 배포한 컨테이너 파일시스템은 재배포 시 휘발됩니다. production에서 `/data` 볼륨 연결은 필수입니다.

Railway 플랫폼 코드는 애플리케이션에 없으며 일반 Docker 이미지와 같습니다. Public Domain에서는 토큰을 충분히 길게 설정하고 검증 종료 후 `DEMO_MODE=false`, 필요 시 Domain 삭제를 수행합니다.

Railway는 연결된 GitHub 저장소 루트의 `.env.example`을 스캔해 변수 후보를 제안합니다. 새 변수가 보이지 않으면 이 변경이 포함된 브랜치를 먼저 push한 뒤 서비스의 Variables 탭을 새로 열거나, RAW Editor에 `.env.example` 내용을 붙여 넣습니다. 빈 `KMA_SERVICE_KEY`는 예보 연동을, 빈 `KMA_APIHUB_KEY`는 레이더·태풍·특보 연동을 비활성화하므로 사용할 자료의 실제 인증키로 교체해야 합니다.
