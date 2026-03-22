# AssiHarness PRD

## 1. Overview

**AssiHarness**는 설정 기반 범용 에이전트 오케스트레이션 엔진이다.
GitHub Issues/PR/CI 이벤트와 주기적 스케줄을 입력으로 받아, 적절한 Claude Code 워커를 선택하고, 격리된 Git worktree에서 실행시킨 뒤, 결과를 다시 GitHub 상태로 환원한다.

- **언어**: Go (크로스 플랫폼 단일 바이너리 배포)
- **실행 방식**: Go에서 `claude -p`를 subprocess(`os/exec`)로 호출. Agent SDK(Python/TypeScript 전용)는 사용하지 않고, CLI 기반으로 모든 플랫폼에서 동작.
- **핵심 원칙**: Agent-agnostic — 코어 소스코드에 특정 에이전트 이름(dev, qa, qc 등)이 등장하지 않는다. 에이전트가 1개든 100개든 동일한 바이너리로 구동한다.

## 2. 용어 정의

| 용어 | 정의 |
|------|------|
| **AssiHarness** | 전체 오케스트레이션 시스템 (이 프로젝트) |
| **AssiLoop** | AssiHarness 내부의 메인 실행 루프 컴포넌트 |
| **Agent** | config 파일로 정의되는 실행 단위. Claude Code `-p` 모드로 실행됨 |
| **Worker** | Agent가 실행 중인 인스턴스 |
| **Task** | Agent에 전달되는 작업 단위 (Issue, PR, Schedule job 등에서 파생) |
| **Route** | 이벤트/상태 조건과 실행할 Agent를 매핑하는 규칙 |
| **Run** | 하나의 Task에 대한 Worker 실행 기록 |

## 3. 아키텍처

```
[Event Sources]                        [Config Files]
 ├─ GitHub Issues                       ├─ config/agents/*.yml
 ├─ GitHub PRs                          ├─ config/routes.yml
 ├─ CI Results                          ├─ config/schedules.yml
 ├─ Schedule (내부 스케줄러)              ├─ config/policies.yml
 └─ External Signals                    └─ prompts/*.md

        ↓                                      ↓

              ┌──────────────────────┐
              │   AssiHarness Core   │
              │                      │
              │  ├─ Config Loader    │
              │  ├─ Agent Registry   │
              │  ├─ Route Engine     │
              │  ├─ Schedule Engine  │
              │  ├─ Runner           │
              │  ├─ Worktree Manager │
              │  ├─ State Store      │
              │  └─ Recovery Engine  │
              └──────────┬───────────┘
                         │
                         ↓

[Agents (config-defined)]         [Outputs]
 ├─ agent-a                        ├─ GitHub Issue (생성/라벨/코멘트)
 ├─ agent-b                        ├─ GitHub PR (생성/업데이트)
 ├─ agent-c                        ├─ Project State 반영
 ├─ ...                            ├─ Data Store (파일/DB)
 └─ agent-n                        └─ Logs / Metrics
```

## 4. 설계 원칙

1. **Agent-Agnostic**: 코어 코드에 `if agent == "dev"` 같은 분기 없음. Agent Registry가 config 파일을 읽어 동적으로 등록.
2. **Config-Driven**: 모든 행동 차이는 YAML config와 Markdown prompt 파일에서 정의.
3. **소스 수정 없는 확장**: 새 에이전트 추가 = `config/agents/new.yml` + `prompts/new.md` + routes/schedules 수정. 바이너리 재빌드 불필요.
4. **GitHub = State Store**: 별도 큐(Redis, SQLite) 없이 GitHub Issues/PR/Labels/Projects를 상태 저장소로 사용.
5. **Worktree 격리**: 모든 Worker는 독립 Git worktree에서 실행. 동일 worktree를 두 Worker가 동시에 사용하지 않음.
6. **Stateless Worker**: Worker는 매번 `claude -p`로 실행하고 종료. 상태는 GitHub과 State Store에만 존재.
7. **Go 단일 바이너리**: 크로스 플랫폼. macOS, Linux, Windows에서 의존성 없이 실행.

## 5. 코어 컴포넌트

### 5.1 Config Loader

시작 시 config 디렉터리를 읽어 모든 설정을 메모리에 로드한다. 런타임 중 파일 변경 시 hot-reload를 지원한다.

- `config/agents/*.yml` — 에이전트 정의
- `config/routes.yml` — 라우팅 규칙
- `config/schedules.yml` — 주기 작업
- `config/policies.yml` — 공통 정책 (retry, timeout, issue 생성 규칙)
- `config/runtime.yml` — 런타임 설정 (poll interval, log level, GitHub 연결 정보)

### 5.2 Agent Registry

`config/agents/*.yml`을 읽어 에이전트 목록을 구성한다.
코어는 아래 공통 스키마만 알면 된다:

```yaml
# config/agents/example.yml
id: example-agent
type: claude_worker          # 실행기 타입 (claude_worker, script, webhook)
enabled: true

prompt_file: prompts/example.md
allowed_tools:
  - Read
  - Write
  - Edit
  - Bash
  - Glob
  - Grep

worktree:
  mode: per_task             # per_task | shared_role
  pattern: "{agent_id}-{task_id}"

concurrency:
  max_parallel: 3

timeouts:
  execution: 30m
  claim: 5m

retries:
  max_attempts: 2
  backoff: 1m

outputs:
  on_success:
    add_labels: ["cc:done"]
    remove_labels: ["cc:running"]
  on_failure:
    add_labels: ["cc:failed"]
    remove_labels: ["cc:running"]
```

### 5.3 Route Engine

이벤트를 어떤 Agent로 보낼지 결정한다. 규칙은 모두 `config/routes.yml`에 정의된다.

```yaml
# config/routes.yml
routes:
  - id: issue_to_dev
    when:
      source: github_issue
      labels: ["cc:dev", "cc:ready"]
    dispatch:
      agent: dev

  - id: pr_to_qc
    when:
      source: github_pr
      labels: ["pr:qc"]
    dispatch:
      agent: qc

  - id: ci_fail_to_issue
    when:
      source: ci_result
      status: failed
    dispatch:
      agent: issue-reporter

  - id: schedule_collector
    when:
      source: schedule
      job: collect_logs
    dispatch:
      agent: collector
```

내부적으로 모든 이벤트는 정규화된 Event 객체로 변환된다:

```go
type Event struct {
    Source   string            // github_issue, github_pr, ci_result, schedule
    SourceID string           // issue number, PR number, job id
    Labels   []string
    Status   string
    Payload  map[string]any
}
```

### 5.4 Schedule Engine

`config/schedules.yml`을 읽어 주기 작업을 관리한다.

```yaml
# config/schedules.yml
jobs:
  - id: collect_logs
    enabled: true
    every: 5m
    dispatch:
      agent: collector
      input:
        source: app_logs

  - id: ontology_plan
    enabled: true
    every: 1h
    dispatch:
      agent: ontology-planner
      input:
        source: collected_artifacts

  - id: ontology_exec
    enabled: false
    every: 2h
    dispatch:
      agent: ontology-executor
```

상태 관리:
- `state/schedules.json`에 `last_run_at`, `next_run_at`, `running`, `consecutive_failures` 기록
- 연속 실패 시 `policies.yml`의 규칙에 따라 ops 이슈 발행

### 5.5 Runner

Agent를 실제로 실행하는 컴포넌트. Agent type별 실행 방식:

| type | 실행 방식 |
|------|-----------|
| `claude_worker` | `claude -p --worktree <name> --append-system-prompt-file <prompt> --tools <tools> --output-format json "<instruction>"` |
| `script` | 지정된 shell script 실행 |
| `webhook` | HTTP POST 호출 |

**Worktree 실행 전략:**
- `claude -p --worktree <name>` 조합은 직접 테스트로 정상 동작 확인됨 (2026-03-23 검증)
- Claude가 `.claude/worktrees/<name>` 아래에 worktree를 자동 생성하고, `worktree-<name>` 브랜치를 자동 할당
- `-p` 모드에서는 종료 시 자동 정리 프롬프트가 나오지 않으므로, AssiHarness가 완료 후 `git worktree remove` + `git branch -D`로 정리해야 함
- Worktree Manager의 역할이 "생성"에서 "정리"로 단순화됨

**결과 판정:**
- `claude -p`의 exit code는 항상 신뢰할 수 없음 (알려진 이슈)
- `--output-format json`으로 실행하고, JSON 응답의 `result` 필드를 파싱하여 성공/실패 판정
- Runner는 exit code와 JSON 결과를 모두 확인하는 이중 판정 로직 사용

**CLI 플래그 참고:**

| 플래그 | 역할 |
|--------|------|
| `--tools "Read,Write,Bash"` | 사용 가능한 도구를 **제한** (allowlist) |
| `--allowedTools "Bash(git *)"` | 특정 도구를 권한 프롬프트 없이 **자동 승인** |
| `--disallowedTools "Write"` | 특정 도구를 완전 **차단** |

Runner는 Agent type을 인터페이스로 추상화한다:

```go
type Runner interface {
    Run(ctx context.Context, agent AgentConfig, task Task) (RunResult, error)
}
```

### 5.6 Worktree Manager

Worktree 점유 확인, 정리, 이름 생성을 담당한다.
생성은 `claude -p --worktree <name>`이 자동으로 수행하므로, AssiHarness는 직접 `git worktree add`를 호출하지 않는다.

**정책 (agent config에서 정의):**

| mode | 설명 | 용도 |
|------|------|------|
| `per_task` | Task마다 고유 이름 생성 (`dev-issue-123`) | 코드 수정이 많은 에이전트 (dev, qc) |
| `shared_role` | Role별 고정 이름 재사용 (`collector-logs`) | 읽기 중심 에이전트 (collector, planner) |

**규칙:**
- 동일 worktree 이름으로 두 Worker가 동시에 실행되지 않는다 (점유 체크).
- Claude가 `.claude/worktrees/<name>` 아래에 worktree를 자동 생성하고, `worktree-<name>` 브랜치를 할당.
- `-p` 모드에서는 종료 시 자동 정리가 되지 않으므로, AssiHarness가 완료 후 정리 수행:
  - 성공 + PR 생성 → worktree 보존 (PR 머지 후 정리)
  - 성공 + 변경 없음 → 즉시 `git worktree remove` + `git branch -D`
  - 실패 → policies.yml의 `cleanup_after` 경과 후 정리

### 5.7 State Store

실행 상태를 로컬 파일로 관리한다.

```
state/
├─ runtime.json       # AssiHarness 메인 루프 상태
├─ schedules.json     # 각 schedule job의 last_run, next_run
├─ runs/              # 각 Run의 기록 (agent, task, result, duration)
├─ heartbeats/        # 각 활성 Worker의 heartbeat
└─ worktrees.json     # worktree 점유 상태
```

### 5.8 Recovery Engine

비정상 상태를 복구한다.

- `cc:running` 라벨이 붙은 채 timeout을 초과한 이슈 → `cc:failed`로 전환 또는 재시도
- orphaned worktree 정리
- 연속 실패 schedule job에 대한 ops 이슈 발행
- AssiHarness 재시작 시 stale 상태 정리

## 6. GitHub 연동

### 6.1 Labels (상태 + 라우팅)

**상태 라벨:**

| Label | 의미 |
|-------|------|
| `cc:ready` | 실행 대기 |
| `cc:running` | 처리 중 |
| `cc:review` | 검토 필요 |
| `cc:blocked` | 외부 입력 대기 |
| `cc:failed` | 실패 |
| `cc:done` | 완료 |

**역할 라벨 (예시, config으로 자유롭게 정의):**

| Label | 의미 |
|-------|------|
| `cc:dev` | 개발 작업 |
| `cc:test` | 테스트 |
| `cc:qa` | QA 검증 |
| `cc:qc` | 품질 점검 |
| `cc:improve` | 개선 |
| `cc:insight` | 온톨로지/데이터 분석 결과 |
| `cc:ops` | 운영 이슈 |

**소스 라벨:**

| Label | 의미 |
|-------|------|
| `src:human` | 사람이 생성 |
| `src:qa` | QA Worker가 생성 |
| `src:qc` | QC Worker가 생성 |
| `src:planner` | Ontology Planner가 생성 |
| `src:ci` | CI 실패로 생성 |

### 6.2 Issue Lifecycle

```
[Issue Created]
    cc:ready + cc:dev
        ↓
[AssiHarness claims]
    cc:running, assignee 설정, "claimed" comment
        ↓
[Worker 실행]
    claude -p --worktree dev-issue-123
        ↓
[성공]                          [실패]
    cc:done                       cc:failed
    PR 생성                       실패 comment
    ↓                             ↓
[QC/QA route]                  [재시도 or 사람 개입]
    pr:qc → QC Worker
    pr:qa → QA Worker
        ↓
[QA 실패 시]
    새 bug issue 생성 (src:qa)
    → 다시 AssiHarness가 라우팅
```

### 6.3 Lock 전략

GitHub Issue를 큐처럼 쓸 때 동시 claim 방지:
1. `cc:ready` 제거 + `cc:running` 추가 (라벨)
2. assignee를 bot 계정으로 설정
3. "claimed by AssiHarness at ..." comment

### 6.4 Project 연동 (선택)

GitHub Projects를 운영 대시보드로 활용:

| Custom Field | 용도 |
|-------------|------|
| Type | bug / feature / improve / insight / task / ops |
| Source | human / qa / qc / collector / planner / ci |
| Worker | 실행 중인 agent id |
| Stage | ready / running / review / blocked / done / failed |
| Priority | p0 / p1 / p2 / p3 |

### 6.5 온프레미스 지원

GitHub Enterprise Server (GHES) 3.20+에서 Issues, Projects, Actions 모두 사용 가능.
`config/runtime.yml`에서 GitHub API endpoint를 설정:

```yaml
# config/runtime.yml
github:
  api_url: https://github.example.com/api/v3
  upload_url: https://github.example.com/api/uploads
  owner: myorg
  repo: myproject
```

## 7. 전체 루프 (AssiLoop)

메인 루프의 매 iteration:

```
1. config 변경 확인 (hot-reload)
2. GitHub Issues/PR 상태 수집 (Event Adapter)
3. Schedule due 여부 확인 (Schedule Engine)
4. 활성 Worker 상태 확인 (heartbeat, timeout)
5. Recovery 처리 (stale runs, orphaned worktrees)
6. 실행 가능 Task 후보 추출
7. Route 규칙으로 Agent 선택
8. concurrency 제한 확인
9. Worktree 할당
10. Worker 실행 (비동기)
11. 결과 반영 (라벨, comment, PR, Project)
12. Sleep (poll_interval)
13. 반복
```

## 8. 설정 파일 전체 스키마

### 8.1 Agent 정의 (`config/agents/<id>.yml`)

```yaml
id: string                    # 고유 식별자
type: claude_worker | script | webhook
enabled: bool

prompt_file: string           # prompts/ 내 파일 경로
allowed_tools: [string]       # --tools에 전달: 사용 가능 도구 제한 (allowlist)
auto_approve_tools: [string]  # --allowedTools에 전달: 권한 프롬프트 없이 자동 승인
disallowed_tools: [string]    # --disallowedTools에 전달: 완전 차단
extra_flags: [string]         # 추가 CLI 플래그 (--verbose 등)

worktree:
  mode: per_task | shared_role
  pattern: string             # 템플릿: {agent_id}, {task_id}, {issue_number}

concurrency:
  max_parallel: int           # 이 에이전트의 최대 동시 실행 수

timeouts:
  execution: duration         # Worker 실행 제한 시간
  claim: duration             # claim 후 실행 시작까지 허용 시간

retries:
  max_attempts: int
  backoff: duration

can_create_issues: bool       # 이 에이전트가 GitHub Issue를 생성할 수 있는지
issue_creation_rules:         # can_create_issues: true일 때
  check_duplicates: bool
  required_labels: [string]
  severity_threshold: string  # low | medium | high | critical

outputs:
  on_success:
    add_labels: [string]
    remove_labels: [string]
  on_failure:
    add_labels: [string]
    remove_labels: [string]
```

### 8.2 Routes (`config/routes.yml`)

```yaml
routes:
  - id: string
    when:
      source: github_issue | github_pr | ci_result | schedule
      labels: [string]        # AND 조건
      status: string          # ci: passed/failed, pr: opened/merged
      job: string             # schedule job id
    dispatch:
      agent: string           # agent id
      input: map              # 추가 입력 데이터
    priority: int             # 낮을수록 높은 우선순위 (기본 100)
```

### 8.3 Schedules (`config/schedules.yml`)

```yaml
jobs:
  - id: string
    enabled: bool
    every: duration           # 5m, 1h, 30s 등
    dispatch:
      agent: string
      input: map
```

### 8.4 Policies (`config/policies.yml`)

```yaml
lock:
  strategy: label_and_assignee     # label_only | assignee_only | label_and_assignee
  bot_user: assiharness-bot

worktree:
  cleanup_after: 24h               # per_task worktree 보존 기간
  max_total: 20                    # 전체 worktree 수 제한

recovery:
  stale_running_timeout: 1h        # cc:running 상태로 이 시간 초과 시 복구
  max_consecutive_failures: 3      # schedule job 연속 실패 시 ops 이슈 발행
  orphan_check_interval: 30m

issue_creation:
  duplicate_check: true            # 이슈 생성 전 제목 유사도 검사
  loop_prevention:
    max_issues_per_hour: 10        # 에이전트별 시간당 이슈 생성 제한
    cooldown_on_limit: 30m
```

### 8.5 Runtime (`config/runtime.yml`)

```yaml
poll_interval: 30s
log_level: info                    # debug | info | warn | error
state_dir: state/
logs_dir: logs/

github:
  api_url: https://api.github.com  # 또는 GHES URL
  owner: string
  repo: string
  # 인증은 환경변수 GITHUB_TOKEN 사용

claude:
  binary: claude                    # claude CLI 경로
  default_output_format: json       # text | json | stream-json
  default_verbose: false
```

## 9. 프로젝트 디렉터리 구조

```
assiharness/
├── cmd/
│   └── assiharness/
│       └── main.go                 # 진입점
├── internal/
│   ├── config/
│   │   ├── loader.go               # Config Loader (hot-reload)
│   │   └── schema.go               # 설정 구조체 정의
│   ├── registry/
│   │   └── registry.go             # Agent Registry
│   ├── router/
│   │   └── router.go               # Route Engine
│   ├── scheduler/
│   │   └── scheduler.go            # Schedule Engine
│   ├── runner/
│   │   ├── runner.go               # Runner 인터페이스
│   │   ├── claude.go               # Claude Worker Runner
│   │   ├── script.go               # Script Runner
│   │   └── webhook.go              # Webhook Runner
│   ├── worktree/
│   │   └── manager.go              # Worktree Manager
│   ├── state/
│   │   └── store.go                # State Store (파일 기반)
│   ├── recovery/
│   │   └── recovery.go             # Recovery Engine
│   ├── adapter/
│   │   ├── github.go               # GitHub Issue/PR/Label/Comment 어댑터
│   │   └── project.go              # GitHub Projects 어댑터 (선택)
│   ├── loop/
│   │   └── loop.go                 # AssiLoop 메인 루프
│   └── models/
│       └── models.go               # Event, Task, Run, AgentConfig 등
├── config/
│   ├── agents/                     # 에이전트 정의 파일들
│   │   └── .gitkeep
│   ├── routes.yml
│   ├── schedules.yml
│   ├── policies.yml
│   └── runtime.yml
├── prompts/                        # 에이전트 프롬프트 파일들
│   └── .gitkeep
├── state/                          # 런타임 상태 (gitignore)
│   ├── runtime.json
│   ├── schedules.json
│   ├── worktrees.json
│   └── runs/
├── logs/                           # 로그 (gitignore)
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 10. 빌드 및 실행

### 빌드

```bash
# 현재 플랫폼
make build

# 크로스 컴파일
make build-all    # linux/amd64, darwin/arm64, windows/amd64
```

### 실행

```bash
# 기본 실행
./assiharness

# config 디렉터리 지정
./assiharness --config ./config

# dry-run (실행 없이 라우팅 결과만 출력)
./assiharness --dry-run

# 단일 실행 후 종료 (CI/cron용)
./assiharness --once
```

### 프로세스 관리

```bash
# macOS: launchd로 상시 실행
launchctl load ~/Library/LaunchAgents/com.assi.assiharness.plist

# Linux: systemd 서비스
sudo systemctl enable assiharness
sudo systemctl start assiharness

# 또는 tmux/screen
tmux new-session -d -s assiharness './assiharness'
```

## 11. 운영 루프 예시

### 예시: 데이터 수집 → 분석 → 개발 → 검증 루프

```
[5분마다 Collector 실행]
    → app_logs 수집 → data/ 저장
        ↓
[1시간마다 Ontology Planner 실행]
    → data/ 읽기 → 패턴 발견
    → GitHub Issue 발행: "High login failure rate on mobile"
      라벨: cc:insight, cc:dev, cc:ready
        ↓
[AssiHarness 감지]
    → Route: cc:dev + cc:ready → dev agent
    → Worktree: dev-issue-42 생성
    → claude -p 실행
        ↓
[Dev Worker 완료]
    → PR #88 생성, cc:done
    → Route: PR opened → pr:qc → QC agent
        ↓
[QC Worker 실행]
    → lint/typecheck/review
    → 심각한 문제 발견 → Issue 발행 (src:qc, cc:improve)
    → 또는 PR comment만 남김
        ↓
[QA Worker 실행]
    → Acceptance criteria 검증
    → 통과 → cc:done
    → 실패 → Issue 발행 (src:qa, cc:bug, cc:ready)
        ↓
[다시 AssiHarness 라우팅 → Dev Worker → ...]
```

## 12. 구현 로드맵

### Phase 1: Core Engine

- [ ] Go 프로젝트 초기화 (`go mod init`)
- [ ] Config Loader (YAML 파싱, agents/routes/schedules/policies/runtime)
- [ ] Agent Registry (config에서 동적 로드)
- [ ] Models (Event, Task, Run, AgentConfig)
- [ ] GitHub Adapter (gh CLI 래퍼: issue list, label, comment, PR)
- [ ] Route Engine (조건 매칭, agent dispatch)
- [ ] Claude Runner (`claude -p --worktree` 실행)
- [ ] Worktree Manager (생성, 점유, 정리)
- [ ] State Store (파일 기반 JSON)
- [ ] AssiLoop 메인 루프 (poll → route → run → record)

### Phase 2: Reliability

- [ ] Schedule Engine (주기 작업 실행)
- [ ] Recovery Engine (stale run, orphaned worktree, 연속 실패)
- [ ] Lock 전략 (label + assignee + comment)
- [ ] Heartbeat (Worker 생존 확인)
- [ ] Config hot-reload (파일 감시)
- [ ] Structured logging

### Phase 3: Extensibility

- [ ] Script Runner (임의 shell script 실행)
- [ ] Webhook Runner (HTTP POST)
- [ ] GitHub Projects Adapter
- [ ] Issue 생성 규칙 엔진 (중복 방지, severity threshold, loop prevention)
- [ ] CLI 서브커맨드 (`assiharness status`, `assiharness run-once`, `assiharness validate-config`)
- [ ] Dry-run 모드

### Phase 4: Operations

- [ ] 크로스 컴파일 (linux/darwin/windows)
- [ ] launchd/systemd 서비스 파일 제공
- [ ] Prometheus metrics endpoint (선택)
- [ ] 웹 GUI (config 편집 전용, 선택)
