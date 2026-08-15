# 운영 지침
- 상태: `/health/live`는 프로세스, `/health/ready`는 주 DB와 자산 디렉터리 쓰기 상태입니다. 데이터 지연은 `/api/v1/status`의 `normal/stale/disconnected`로 별도 감시합니다. 외부 연동을 켠 경우 `collectors`에서 KMA 예보와 API Hub 각 제품의 마지막 시도·성공·실패 여부를 따로 확인합니다.
- 로그: stdout JSON이며 플랫폼/호스트가 회전합니다. request ID, 경로, 처리시간을 남기고 토큰/Base64는 남기지 않습니다.
- 정리: 시작 시와 24시간마다 이력 30일, 레이더 72시간 기준으로 정리하며 최신 레이더 한 장은 보호합니다.
- directory: 10초 간격으로 `*.json`을 처리해 성공은 삭제, 실패는 `/data/quarantine`으로 이동합니다. directory 모드에서는 HTTP 수신 API가 비활성화됩니다.
- 장애: ready 실패 시 디렉터리 권한/용량/SQLite 로그를 확인합니다. 손상 DB는 자동 삭제하지 않습니다. 운영자가 서비스를 중지하고 `weather.db*`를 `weather.db.corrupt-날짜`로 격리한 뒤 재기동하면 스키마와 sites가 생성됩니다.
- 백업은 필수가 아니나 점검 전 컨테이너 중지 후 volume snapshot 또는 SQLite 파일 묶음을 선택적으로 보관합니다.
