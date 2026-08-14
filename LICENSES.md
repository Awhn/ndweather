# 제3자 라이선스 및 SBOM
직접 의존성은 Go 표준 라이브러리, modernc.org/sqlite(BSD-3-Clause 계열), yaml.v3(MIT/Apache 고지), React/ReactDOM/Vite/TypeScript 및 시험 도구(MIT 계열)입니다. 정확한 전이 의존성과 라이선스는 고정 lock 파일을 기준으로 릴리스마다 생성합니다.
```bash
go install github.com/google/go-licenses@latest
go-licenses report ./backend/cmd/weather > go-licenses.csv
npx license-checker --production --csv > npm-licenses.csv
syft internal-weather-system:1.0.0 -o spdx-json > sbom.spdx.json
```
오프라인 반입 패키지에 라이선스 보고서와 SBOM을 포함합니다.
