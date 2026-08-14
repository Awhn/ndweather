# Railway 검증 배포
1. GitHub 저장소를 Railway 프로젝트의 서비스 한 개에 연결합니다.
2. 빌더가 루트 `Dockerfile`을 감지하고 이미지 빌드 로그가 성공하는지 확인합니다.
3. Volume 하나를 만들고 **mount path `/data`** 로 서비스에 연결합니다.
4. Variables에 `.env.example` 값을 입력합니다. 필수값은 `INGEST_MODE=http`, 32자 이상 난수 `INGEST_TOKEN`, `DEMO_MODE=true`(검증 시), `DATA_DIR=/data`, `SQLITE_PATH=/data/weather.db`, `ASSET_DIR=/data/assets`, `INBOX_DIR=/data/inbox`입니다. `PORT`는 Railway 주입값을 그대로 둡니다.
5. Healthcheck path를 `/health/ready`로, timeout을 30초 이상으로 지정합니다.
6. 검증용 Public Domain을 생성합니다. 운영 업무망에는 공개 도메인을 사용하지 않습니다.
7. `BASE_URL=https://... INGEST_TOKEN='...' ./scripts/send-demo-batch.sh`로 전송하고 HTTP 200을 확인합니다.
8. `https://.../display`에서 두 화면, DEMO DATA, 5초 회전을 확인합니다.
9. redeploy 후 `/api/v1/observations/latest`와 `/data/assets` 메타데이터가 유지되는지 확인합니다.
10. Volume 없이 배포한 컨테이너 파일시스템은 재배포 시 휘발됩니다. production에서 `/data` 볼륨 연결은 필수입니다.

Railway 플랫폼 코드는 애플리케이션에 없으며 일반 Docker 이미지와 같습니다. Public Domain에서는 토큰을 충분히 길게 설정하고 검증 종료 후 `DEMO_MODE=false`, 필요 시 Domain 삭제를 수행합니다.
