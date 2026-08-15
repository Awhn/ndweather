# API 규약
모든 v1 성공 응답은 `apiVersion`, `generatedAt`, `data`를 포함합니다. 읽기 API와 레이더 자산은 `READ_API_TOKEN`이 비어 있지 않을 때 Bearer 인증을 요구합니다. `/display`의 정적 화면과 `/health/*`는 공개이지만 토큰 설정 시 화면은 `/display#token=<READ_API_TOKEN>`으로 열어야 데이터를 조회할 수 있습니다. 수신은 HTTP 모드에서 강한 `INGEST_TOKEN` 필수이며 directory 모드에서는 HTTP 수신 경로를 등록하지 않습니다. 알 수 없는 JSON 필드, 잘못된 RFC3339, 크기·SHA·MIME·매직바이트 오류를 원자적으로 거부합니다. 오류는 `data.code`로 식별하며 내부 경로를 반환하지 않습니다.

목록은 기본 100, 최대 500건입니다. 관측 기간 기본값은 최근 7일이며 `since`와 `limit`을 지원하고 최신순으로 정렬합니다. 레이더 frames는 `limit`을 지원합니다. 동일 batchId는 기존 결과와 `duplicate:true`를 반환합니다. 향후 청크 수신은 별도 `/api/v2/uploads` 세션, 청크 해시와 최종 manifest 검증 방식으로 추가하고 v1 의미는 유지합니다.
