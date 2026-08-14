# 보안 설계
Bearer token은 constant-time 비교하며 수신 요청 크기, 파일 수/크기, Base64, SHA-256, MIME과 매직바이트를 검증합니다. 서버 생성 파일명과 고정 asset 디렉터리를 사용하고 업로드 실행 권한을 주지 않습니다. SQL은 바인딩하며 unknown JSON 필드는 거부합니다. CORS 헤더를 내보내지 않아 동일 오리진만 사용하고 CSP, nosniff, no-referrer, SAMEORIGIN을 적용합니다. 컨테이너는 UID 10001, read-only root/no-new-privileges/cap-drop 구성이 가능합니다.

릴리스 전 `go test ./...`, `npm test`, `govulncheck ./...`, `npm audit --omit=dev`, `docker scout cves` 또는 Trivy를 실행해 Critical/High가 0인지 확인합니다. `syft internal-weather-system:1.0.0 -o cyclonedx-json > sbom.json`으로 SBOM을 생성하며 결과는 릴리스 증적에 보관하고 비밀/운영 데이터는 Git에 넣지 않습니다.
