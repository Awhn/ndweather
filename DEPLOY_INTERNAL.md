# 폐쇄 업무망 이전
## 외부 빌드·검증·반입
```bash
docker build -t internal-weather-system:1.0.0 .
docker save internal-weather-system:1.0.0 -o internal-weather-system_1.0.0.tar
sha256sum internal-weather-system_1.0.0.tar > internal-weather-system_1.0.0.tar.sha256
```
두 파일을 승인 매체로 반입한 뒤 업무망에서 `sha256sum -c ...sha256`, `docker load -i ...tar`를 실행합니다. `.env.internal.example`을 `.env.internal`로 복사해 강한 토큰을 입력합니다. named volume은 Compose가 생성하며 UID 10001 권한 문제가 있는 bind mount라면 `install -d -o 10001 -g 10001 -m 0750 /srv/ndweather` 후 volume 경로를 바꿉니다.

```bash
sha256sum -c internal-weather-system_1.0.0.tar.sha256
docker load -i internal-weather-system_1.0.0.tar
docker compose -f docker-compose.internal.yml up -d
```
방화벽은 송신 연계 시스템과 TV/조회 단말에서 `${HOST_IP}:${HOST_PORT}` TCP만 허용합니다. IP/포트는 `.env.internal`의 `HOST_IP`, `HOST_PORT`만 바꾸고 재생성합니다. 컨테이너 내부는 항상 8080입니다.

## 운용
`up -d`, `stop`, `restart weather-system`, `logs -f --tail=200 weather-system`, `down`을 사용합니다. 초기화는 반드시 승인 후 `down -v`(전체 이력 삭제)합니다. 인터넷 차단 시험은 `docker compose ... down`, 호스트 uplink 차단, 다시 `up -d`한 뒤 health/display/API와 기존 데이터 확인으로 수행합니다. Compose는 read-only root, `/tmp` tmpfs, capabilities 제거, non-root UID를 적용합니다. HTTP directory 모드라면 `/data/inbox`에 같은 파일시스템 rename으로 완성된 UTF-8 JSON만 넣습니다.
