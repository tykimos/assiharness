# AssiHarness

설정 기반 범용 에이전트 오케스트레이션 엔진입니다. GitHub Issues/PR/CI 이벤트와 주기적 스케줄을 입력으로 받아, Claude Code 워커를 격리된 Git worktree에서 실행하고, 결과를 GitHub에 반영합니다.

## 특징

- **Agent-Agnostic**: 코어 코드에 특정 에이전트 이름이 등장하지 않습니다. 에이전트는 YAML config로 정의됩니다.
- **Config-Driven**: 새 에이전트 추가 = config 파일 추가. 바이너리 재빌드 불필요.
- **Go 단일 바이너리**: macOS, Linux, Windows에서 의존성 없이 실행.
- **Worktree 격리**: 모든 Worker는 독립 Git worktree에서 실행. 동시 작업 간 충돌 없음.
- **GitHub = State Store**: 별도 큐(Redis, SQLite) 없이 GitHub Issues/Labels를 상태 저장소로 활용.

## 사전 요구사항

시작하기 전에 아래 도구가 설치되어 있어야 합니다:

### 1. Go 설치 (1.21 이상)

```bash
# macOS (Homebrew)
brew install go

# 설치 확인
go version
# 출력 예: go version go1.26.0 darwin/arm64
```

### 2. Claude Code CLI 설치

```bash
# npm으로 설치
npm install -g @anthropic-ai/claude-code

# 설치 확인
claude --version
```

### 3. GitHub CLI (gh) 설치 및 인증

```bash
# macOS (Homebrew)
brew install gh

# GitHub 로그인
gh auth login
# → GitHub.com 선택 → HTTPS → 브라우저 인증

# 인증 확인
gh auth status
```

## 빠른 시작 (Quick Start)

### Step 1: 저장소 클론 및 빌드

```bash
# 저장소 클론
git clone https://github.com/tykimos/assiharness.git
cd assiharness

# 빌드
make build

# 빌드 확인
./assiharness --help
```

출력:

```
Usage of ./assiharness:
  -config string
        path to config directory (default "config")
  -dry-run
        print routing results without executing
  -once
        run single iteration and exit
```

### Step 2: GitHub 저장소 설정

`config/runtime.yml`에서 자신의 GitHub 저장소 정보를 설정합니다:

```yaml
# config/runtime.yml
poll_interval: 30s
log_level: info
state_dir: state
logs_dir: logs

github:
  api_url: https://api.github.com
  owner: 자신의_GitHub_아이디    # ← 변경
  repo: 자신의_저장소_이름       # ← 변경

claude:
  binary: claude
  default_output_format: json
  default_verbose: false
```

### Step 3: GitHub 라벨 생성

AssiHarness가 사용하는 라벨을 GitHub 저장소에 생성합니다:

```bash
# 상태 라벨
gh label create "cc:ready"   --color "1D76DB" --description "Ready for processing"
gh label create "cc:running" --color "FBCA04" --description "Currently processing"
gh label create "cc:done"    --color "0E8A16" --description "Completed"
gh label create "cc:failed"  --color "D93F0B" --description "Failed"

# 역할 라벨 (에이전트 라우팅용)
gh label create "cc:dev"     --color "0E8A16" --description "Development task"
```

### Step 4: Dry-run 테스트

실제 실행 없이 라우팅이 올바른지 확인합니다:

```bash
./assiharness --once --dry-run --config ./config
```

출력 예:

```json
{"time":"...","level":"INFO","msg":"starting","component":"orchestrator","poll_interval":"30s","once":true,"dry_run":true}
{"time":"...","level":"INFO","msg":"--once mode, waiting for active workers to finish"}
```

### Step 5: 실제 실행 테스트

테스트 Issue를 만들고 AssiHarness를 실행합니다:

```bash
# 테스트 이슈 생성
gh issue create \
  --title "Test: Add hello.txt file" \
  --body "Create a file named hello.txt with the content 'Hello from AssiHarness!'" \
  --label "cc:dev" --label "cc:ready"

# AssiHarness 단일 실행
./assiharness --once --config ./config
```

실행 결과:

1. Issue의 `cc:ready` 라벨이 `cc:running`으로 변경됩니다.
2. Claude Code 워커가 worktree에서 작업을 수행합니다.
3. 완료 후 `cc:done` (성공) 또는 `cc:failed` (실패) 라벨이 붙습니다.
4. Issue에 결과 코멘트가 작성됩니다.

GitHub에서 Issue를 확인하면 라벨과 코멘트가 변경된 것을 볼 수 있습니다.

## 상시 실행

### 기본 실행 (포그라운드)

```bash
./assiharness --config ./config
```

30초마다 GitHub을 확인하고, `cc:ready` 이슈가 있으면 자동으로 처리합니다. `Ctrl+C`로 종료합니다.

### tmux로 백그라운드 실행

```bash
tmux new-session -d -s assiharness './assiharness --config ./config'

# 로그 확인
tmux attach -t assiharness

# 세션에서 나가기: Ctrl+B, D
```

### macOS launchd 서비스

```bash
launchctl load ~/Library/LaunchAgents/com.assi.assiharness.plist
```

### Linux systemd 서비스

```bash
sudo systemctl enable assiharness
sudo systemctl start assiharness
```

## 설정 가이드

### 에이전트 추가하기

새 에이전트는 config 파일 2개만 추가하면 됩니다:

#### 1. 에이전트 정의 (`config/agents/새이름.yml`)

```yaml
id: qa                         # 고유 식별자
type: claude_worker             # 실행 타입
enabled: true

prompt_file: prompts/qa.md      # 에이전트 프롬프트
allowed_tools:                  # 사용 가능 도구
  - Read
  - Bash
  - Grep

worktree:
  mode: per_task                # 작업별 독립 worktree
  pattern: "{agent_id}-{task_id}"

concurrency:
  max_parallel: 2               # 최대 동시 실행 수

timeouts:
  execution: 15m                # 실행 제한 시간

retries:
  max_attempts: 1
  backoff: 1m

outputs:
  on_success:
    add_labels: ["cc:done"]
    remove_labels: ["cc:running"]
  on_failure:
    add_labels: ["cc:failed"]
    remove_labels: ["cc:running"]
```

#### 2. 프롬프트 파일 (`prompts/qa.md`)

```markdown
You are a QA agent. Review the code changes and verify they meet the acceptance criteria.
```

#### 3. 라우팅 규칙 추가 (`config/routes.yml`)

```yaml
routes:
  - id: issue_to_dev
    when:
      source: github_issue
      labels: ["cc:dev", "cc:ready"]
    dispatch:
      agent: dev
    priority: 10

  - id: issue_to_qa           # ← 새 route 추가
    when:
      source: github_issue
      labels: ["cc:qa", "cc:ready"]
    dispatch:
      agent: qa
    priority: 20
```

그리고 `cc:qa` 라벨을 GitHub에 생성합니다:

```bash
gh label create "cc:qa" --color "5319E7" --description "QA verification"
```

**바이너리 재빌드 없이** 즉시 적용됩니다 (Config hot-reload).

### 주기 작업 설정 (`config/schedules.yml`)

```yaml
jobs:
  - id: collect_logs
    enabled: true
    every: 5m                   # 5분마다 실행
    dispatch:
      agent: collector
      input:
        source: app_logs

  - id: daily_report
    enabled: true
    every: 24h
    dispatch:
      agent: reporter
```

### 정책 설정 (`config/policies.yml`)

```yaml
lock:
  strategy: label_and_assignee
  bot_user: assiharness-bot     # Issue claim 시 assignee

worktree:
  cleanup_after: 24h            # 실패한 worktree 보존 기간
  max_total: 20                 # 전체 worktree 수 제한

recovery:
  stale_running_timeout: 1h     # 이 시간 이상 running이면 stale로 판정
  max_consecutive_failures: 3
  orphan_check_interval: 30m

issue_creation:
  duplicate_check: true
  loop_prevention:
    max_issues_per_hour: 10
    cooldown_on_limit: 30m
```

## 프로젝트 구조

```
assiharness/
├── cmd/assiharness/main.go          # 진입점
├── internal/
│   ├── adapter/github.go            # GitHub CLI 래퍼
│   ├── config/
│   │   ├── loader.go                # YAML 설정 로드
│   │   ├── schema.go                # Config 구조체
│   │   └── watcher.go               # Config hot-reload
│   ├── heartbeat/heartbeat.go       # Worker 생존 확인
│   ├── logger/logger.go             # 구조화 JSON 로깅
│   ├── models/models.go             # 데이터 모델
│   ├── orchestrator/orchestrator.go # 메인 실행 루프
│   ├── recovery/recovery.go         # Stale run/worktree 복구
│   ├── registry/registry.go         # 에이전트 레지스트리
│   ├── router/router.go             # 이벤트→에이전트 라우팅
│   ├── runner/
│   │   ├── runner.go                # Runner 인터페이스
│   │   └── claude.go                # Claude Worker 구현
│   ├── scheduler/scheduler.go       # 주기 작업 엔진
│   ├── state/store.go               # 실행 기록 저장
│   └── worktree/manager.go          # Worktree 관리
├── config/                          # 설정 파일 디렉터리
│   ├── agents/                      # 에이전트 정의
│   ├── routes.yml                   # 라우팅 규칙
│   ├── schedules.yml                # 주기 작업
│   ├── policies.yml                 # 운영 정책
│   └── runtime.yml                  # 런타임 설정
├── prompts/                         # 에이전트 프롬프트
├── state/                           # 런타임 상태 (gitignore)
├── logs/                            # 로그 (gitignore)
├── go.mod
├── go.sum
└── Makefile
```

## 실행 흐름

```
매 tick (기본 30초):
  1. Recovery: stale run 복구, orphaned worktree 정리
  2. GitHub에서 cc:ready 라벨 Issue/PR 수집
  3. Schedule Engine: due job 확인
  4. Route Engine: 이벤트 → 에이전트 매칭
  5. Concurrency 제한 확인
  6. Issue claim (cc:ready → cc:running)
  7. Worktree 할당
  8. Claude Worker 비동기 실행
  9. 결과 반영 (cc:done / cc:failed + 코멘트)
```

## GitHub 라벨 체계

### 상태 라벨

| Label | 의미 |
|-------|------|
| `cc:ready` | 실행 대기 |
| `cc:running` | 처리 중 |
| `cc:done` | 완료 |
| `cc:failed` | 실패 |

### 역할 라벨 (예시)

| Label | 의미 |
|-------|------|
| `cc:dev` | 개발 작업 |
| `cc:qa` | QA 검증 |
| `cc:qc` | 품질 점검 |

## CLI 옵션

```bash
./assiharness [옵션]

옵션:
  --config <path>    설정 디렉터리 경로 (기본: config)
  --once             1회 실행 후 종료
  --dry-run          실행 없이 라우팅 결과만 출력
```

## 로그

구조화 JSON 로그가 stderr와 `logs/assiharness.log`에 동시 출력됩니다:

```json
{"time":"2026-03-23T02:25:39+09:00","level":"INFO","msg":"running worker","agent":"dev","task":"dev-1","worktree":"dev-dev-1"}
{"time":"2026-03-23T02:25:59+09:00","level":"INFO","msg":"worker succeeded","agent":"dev","task":"dev-1"}
```

## 빌드

```bash
# 현재 플랫폼
make build

# 크로스 컴파일
make build-all    # linux/amd64, darwin/arm64, windows/amd64

# 테스트
make test

# 정리
make clean
```

## 라이선스

MIT License
