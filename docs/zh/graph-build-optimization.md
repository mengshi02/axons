# 图谱构建优化方案

> 基于 竞品 项目的深度研究，对应 axons 的三大核心问题：代码文件筛选、异步构建与进度反馈、大图谱降级展示。

---

## 目录

- [问题一：代码文件筛选](#问题一代码文件筛选)
- [问题二：异步构建 + 实时进度反馈](#问题二异步构建--实时进度反馈)
- [问题三：大图谱降级展示](#问题三大图谱降级展示)
- [复审补充发现](#复审补充发现)
- [优先级与工作量评估](#优先级与工作量评估)

---

## 问题一：代码文件筛选

### 现状

Axons 的 `collect_files.go` 采用三层筛选：

| 层级 | 机制 | 实现 |
|------|------|------|
| Layer 1 | `.gitignore` | `utils.NewGitIgnoreMatcher(rootDir)` |
| Layer 2 | 硬编码黑名单 | `skipDirs`（约 20 项）、`skipExts`（约 30 项）、`skipFiles`（约 5 项）+ `IsDecoyDir` 启发式 |
| Layer 3 | 用户 glob 排除 | `ctx.Opts.ExcludePatterns` |

### 竞品 的做法

竞品 采用**三层筛选 + 用户可覆盖**策略：

1. **硬编码黑名单**（`ignore-service.ts`）：`DEFAULT_IGNORE_LIST`（约 60+ 目录名）、`IGNORED_EXTENSIONS`（约 80+ 扩展名，含复合扩展 `.min.js`）、`IGNORED_FILES`（约 30+ 精确文件名），分类覆盖 VCS / IDE / 依赖 / 构建 / 测试 / 日志 / 生成文件 / 证书 / 数据文件等
2. **`.gitignore` + `.needignore`**：使用 `ignore` 包解析用户自定义规则，`.needignore` 优先级高于 `.gitignore`
3. **用户 `!` 否定覆盖**：用户可以在 `.needignore` 中用 `!__tests__/` 来"解封"硬编码黑名单中的目录，通过 `hasExplicitUnignore` 遍历祖先链实现
4. **语言检测预过滤**（`language-detection.ts`）：`EXTENSION_MAP` 只收录 16 种支持语言的扩展名，非代码文件直接跳过
5. **AST LRU 缓存**（`ast-cache.ts`）：解析过的 AST 使用 LRU 缓存复用，避免重复解析
6. **文件大小限制**（`max-file-size.ts`）：默认 512KB 上限，可通过环境变量配置，硬性上限为 tree-sitter 最大 buffer

### 优化建议

#### 1.1 扩展硬编码黑名单

参考 竞品 的完整列表补充缺失项：

**目录**（`skipDirs` 补充）：
- `third_party`, `3rdparty` — C/C++ 第三方依赖
- `jspm_packages` — jspm 包管理
- `bower_components` — Bower 包管理（历史遗留）
- `.terraform`, `.serverless` — IaC 生成目录
- `generated`, `auto-generated`, `.generated` — 生成代码
- `fixtures`, `snapshots`, `__snapshots__` — 测试 fixture
- `.circleci`, `.gitlab` — CI 配置
- `site-packages` — Python 安装目录
- `eggs`, `.eggs`, `.tox` — Python 打包/测试
- `wheels`, `lib64` — Python 构建
- `.parcel-cache`, `.turbo`, `.svelte-kit` — 前端构建缓存

**扩展名**（`skipExts` 补充）：
- `.wasm`, `.node` — WebAssembly / Native Addon
- `.pem`, `.key`, `.crt`, `.cer`, `.p12`, `.pfx` — 证书/密钥（安全）
- `.csv`, `.tsv`, `.parquet`, `.avro`, `.h5`, `.hdf5`, `.npy`, `.npz`, `.pkl`, `.pickle` — 数据文件
- `.db`, `.sqlite`, `.sqlite3`, `.mdb` — 数据库文件
- `.lock` — Lock 文件
- `.d.ts` — TypeScript 类型声明文件
- `.map` — Source Map
- `.bin`, `.dat`, `.data`, `.raw`, `.iso`, `.img`, `.dmg` — 二进制/磁盘映像

**文件名**（`skipFiles` 补充）：
- `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `composer.lock`, `Gemfile.lock`, `poetry.lock`, `Cargo.lock`, `go.sum`
- `.npmrc`, `.yarnrc`, `.editorconfig`, `.prettierrc`, `.prettierignore`, `.eslintignore`, `.dockerignore`
- `LICENSE`, `LICENSE.md`, `LICENSE.txt`, `CHANGELOG.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`
- `.env`, `.env.local`, `.env.development`, `.env.production`, `.env.test`, `.env.example`

#### 1.2 引入 `.axonsignore` 配置文件

- 支持 `.axonsignore` 用户自定义忽略规则，优先级：`.axonsignore` > `.gitignore` > 硬编码黑名单
- 使用 Go 的 `ignore` 等价包（如 `go-gitignore`）解析
- 支持 `!` 否定模式，允许用户解封硬编码黑名单中的目录（如 `!__tests__/` 或 `!vendor/my-lib/`）
- 否定逻辑：遍历路径祖先链，检查是否有 `!` 规则匹配，`hasExplicitUnignore` 实现参考 竞品

#### 1.3 语言感知过滤

当前 `CollectFiles` 收集**所有非黑名单文件**，但其中很多不是代码文件（如 `.txt`、`.md`、`.yaml` 等）。优化为：

- 在 `CollectFiles` 中，只收集 `ctx.Registry` 已注册语言对应的扩展名文件
- 非注册扩展名文件直接跳过，减少后续无效遍历和内存占用
- 保留 `AllFiles` 统计用于展示，但 `ParseChanges` 只包含可解析文件

#### 1.4 文件大小上限

在 `CollectFiles` 的 `filepath.WalkDir` 回调中，对文件增加大小检查：

```go
// 在文件处理分支中（非目录）
fi, err := d.Info()
if err != nil {
    return nil
}
maxFileSize := int64(1 * 1024 * 1024) // 1MB，可通过 BuildOptions 配置
if fi.Size() > maxFileSize {
    return nil
}
```

- 默认阈值 1MB（竞品 默认 512KB，Go 项目文件通常更大）
- 可通过 `BuildOptions.MaxFileSize` 或环境变量 `AXONS_MAX_FILE_SIZE` 配置
- 硬性上限为 tree-sitter 的最大 buffer size

---

## 问题二：异步构建 + 实时进度反馈

### 现状

| 组件 | 状态 | 问题 |
|------|------|------|
| 异步构建 | ✅ 已有 | `task.TaskManager` goroutine + task 状态追踪 |
| 增量构建 | ✅ 已有 | `detect_changes.go` hash diff |
| SSE 事件体系 | ✅ 已有 | `EventBuildProgress` 类型已定义，`useEventStream.ts` 已有监听 |
| Pipeline 进度回调 | ❌ 缺失 | `PipelineContext` 无 `OnProgress` 字段 |
| handleBuild 推送进度 | ❌ 缺失 | 只广播 `EventBuildComplete`，从未广播 `EventBuildProgress` |
| 前端进度展示 | ❌ 缺失 | `BuildingState` 是纯静态 spinner，不消费 `build_progress` 事件 |

### 竞品 的做法

竞品 采用**依赖拓扑排序 Pipeline + 子进程 + IPC + SSE** 多层架构：

1. **依赖声明式 Pipeline**（`pipeline-phases/`）：每个 Phase 声明 `deps: string[]` 依赖关系，runner 用 Kahn 拓扑排序自动决定执行顺序。新增 Phase 只需创建文件并声明依赖，无需手动插入正确位置。当前 Phase 图：`scan → structure → [markdown, cobol] → parse → [routes, tools, orm] → crossFile → mro → communities → processes`
2. **Worker 子进程**（`analyze-worker.ts`）：`child_process.fork()` 隔离运行，8GB 堆，IPC 发送 `{ type: 'progress', phase, percent, message }`
3. **JobManager**（`analyze-job.ts`）：管理任务生命周期（创建/去重/取消/超时），`EventEmitter` 发出进度事件
4. **SSE 实时推送**（`backend-client.ts`）：前端通过 `streamAnalyzeProgress()` 订阅 SSE
5. **AnalyzeProgress 组件**（`AnalyzeProgress.tsx`）：进度条 + 已用时间 + 取消按钮
6. **Pipeline 回调**（`run-analyze.ts`）：`AnalyzeCallbacks.onProgress(phase, percent, message)` 逐阶段上报，进度映射：
   - 0–60%：Pipeline（Scanning → Parsing → Scope Resolution → Communities）
   - 60–85%：LadybugDB 加载
   - 85–90%：FTS 索引
   - 90–100%：Embeddings
7. **增量构建**（`run-analyze.ts`）：文件 hash diff → 只处理 changed/added/deleted → `extractChangedSubgraph` 只写回变更子图
8. **Shadow Candidates**（`shadow-candidates.ts`）：新增文件可能"窃取"已有文件的模块解析路径（如 `foo.ts` 窃取 `foo.js` 的 import），通过 BFS 展开受影响的 importer
9. **增量写入集三层组合**（`run-analyze.ts`）：增量 DB 写回时，写入集由三层组合计算，确保跨文件边一致性：
   - **Layer 1 — BFS 扩展 importers**：`queryImporters` 查 DB 中 IMPORTS 边，BFS 深度 ≤ 4 层，将变更文件的传递 importers 拉入写入集
   - **Layer 2 — Shadow-seed**：新增文件的模块路径"窃取"已有文件解析，seed 进 BFS frontier
   - **Layer 3 — 1-hop 边界展开**：`computeEffectiveWriteSet` 对新 graph 走 1-hop 边界，拉入 unchanged-side 文件（catch barrel re-export 变更导致的跨文件 CALLS 边更新）
   - 三层结果组合后，**删除集和写入集使用相同的 expanded set**（不对称会导致 DB 损坏）。`extractChangedSubgraph` 只提取写入集对应的子图写回 DB
10. **解析缓存**（`run-analyze.ts`）：`loadParseCache` / `saveParseCache` 基于内容 hash 的缓存
11. **分块并行解析**（`parse-impl.ts`）：文件按 ~2MB 字节预算分 chunk，每个 chunk 分发给 Worker Pool 并行解析（可配置池大小）。Parse Cache 基于 chunk 内容 hash，单个文件变更只失效其所在 chunk。预读并发（`parseChunkConcurrency`）允许 overlap 磁盘 I/O 与 worker 计算

### 优化建议

#### 2.1 Pipeline 进度回调 → EventBroker 桥接

**关键发现**：SSE 管道已完整存在（`EventBroker` + `EventBuildProgress` + `useEventStream`），只需桥接 Pipeline 到 EventBroker。

**后端改动**：

1. 在 `PipelineContext` 中增加进度回调：

```go
// context.go
type PipelineContext struct {
    // ... 现有字段 ...
    
    // 进度回调
    OnProgress func(phase string, percent int, message string)
}
```

2. 在 `Pipeline.Build()` 中为每个 stage 设置进度映射：

```go
// pipeline.go
func (p *Pipeline) Build(ctx context.Context) (*types.BuildResult, error) {
    progress := func(phase string, percent int, msg string) {
        if p.ctx.OnProgress != nil {
            p.ctx.OnProgress(phase, percent, msg)
        }
    }
    
    // Stage 1: CollectFiles  0-5%
    progress("collect", 2, "Collecting files...")
    if err := stages.CollectFiles(p.ctx); err != nil { return nil, err }
    progress("collect", 5, "Files collected")
    
    // Stage 2: DetectChanges 5-10%
    progress("detect", 7, "Detecting changes...")
    if err := stages.DetectChanges(p.ctx); err != nil { return nil, err }
    progress("detect", 10, "Changes detected")
    
    // Stage 3: ParseFiles 10-40%（按文件数百分比细化）
    // ... parse 内部每个文件完成后上报
    
    // Stage 4: InsertNodes 40-60%
    // Stage 5: BuildEdges 60-80%
    // Stage 6: RunAnalyses 80-90%
    // Stage 7: Finalize 90-100%
}
```

3. 在 `handleBuild` 中连接回调到 EventBroker：

```go
// handlers.go
pipeline := graph.NewPipelineWithGlobal(pipelineRepo, s.globalRepo, opts)
pipeline.SetOnProgress(func(phase string, percent int, message string) {
    s.broker.Broadcast(Event{
        Type: EventBuildProgress,
        Data: map[string]interface{}{
            "task_id":     taskID,
            "progress":    percent,
            "message":     message,
            "phase":       phase,
            "project_id":  req.ProjectID,
        },
    })
})
```

#### 2.2 前端 BuildingState 消费 build_progress 事件

**替换静态 spinner 为动态进度组件**：

1. 改造 `BuildingState` 组件，接收 `buildProgress` 状态
2. 在 App.tsx 中连接 `useEventStream` 的 `onBuildProgress` 到 BuildingState
3. 显示当前阶段（parsing / building edges / ...）、百分比进度条、已用时间
4. 增加取消按钮，调用 `DELETE /v1/tasks/{task_id}`

#### 2.3 DetectChanges 全量读文件优化

**问题**：`detect_changes.go:32-42` 全量构建时对每个文件执行 `os.ReadFile(path)` 计算 hash，对大项目是 O(n) 全文件读取的性能灾难。

**竞品 做法**：Scan 和 Parse **严格分离**——Scan 阶段（`scan.ts`）只 `stat` 文件获取 path + size，绝不读内容；Parse 阶段（`parse-impl.ts`）按 chunk 才读取文件内容。增量构建的文件 hash diff 在 Pipeline 执行完毕后（post-pipeline）计算，而非在构建前。

**优化方案**：

- 全量构建时，**直接跳过 hash 计算**，标记所有文件为 changed（反正都要重新解析，读内容算 hash 是纯浪费）
- 增量构建时改用两阶段策略：
  1. `CollectFiles` 只收集路径 + `os.Stat` 获取 mtime/size
  2. `DetectChanges` 用 mtime+size 快速判断（Tier 1），仅 mtime 变化的文件才读内容计算 hash（Tier 2）
- 参考 竞品 的分层检测：Tier 0 Journal → Tier 1 mtime/size → Tier 2 content hash
- 长期目标：将 hash diff 计算移到 Pipeline 执行完毕后（post-pipeline），与 竞品 一致——Pipeline 始终解析全部文件，增量写入集由 post-pipeline 的 hash diff 决定

#### 2.4 增量构建内存优化

**问题**：`insert_nodes.go:36` 增量构建时调用 `ctx.Repo.ListAllNodes()` 将所有已有节点加载到内存，对大项目（10万+ 节点）占用大量内存。

**竞品 做法**：使用 SQL 查询能力，增量时只操作变更子图（`extractChangedSubgraph`），不需要全量节点加载。

**优化方案**：

- 改为按需查询模式：边构建时针对 import/call 目标在 DB 中查询，而非全量加载
- 引入节点查询服务接口：
  ```go
  type NodeLookup interface {
      FindByName(name string) ([]*types.Node, error)
      FindByFile(file string) ([]*types.Node, error)
      FindByQualifiedName(qname string) ([]*types.Node, error)
  }
  ```
- 实现两种模式：内存模式（小项目，全量加载）和 DB 查询模式（大项目，按需查询）
- 阈值可配置（如节点数 > 50000 自动切换到 DB 查询模式）

#### 2.5 Watch → 增量构建打通

**现状**：axons 已有 `watcher.go`（fsnotify）+ `journal.go`（变更日志）+ `IncrementalBuilder`（三层检测），但与 API 构建路径没有打通。

**竞品 做法**：增量构建使用 `incrementalInProgress` dirty flag 保障崩溃恢复安全：
- flag 包含 `{ startedAt: Date.now(), toWriteCount: number }` 元数据，存储在 `meta.json` 中
- **在执行任何破坏性 DB 操作之前设置**，成功完成后清除
- 下次启动检测到 flag 后 `force = true` 触发全量重建
- 配合 `lastCommit` 和 `git status --porcelain`（排除 `.competitors/` 自身变更）实现 up-to-date 快速返回

**优化方案**：

1. 将 Watcher 的 `OnChange` 回调连接到 Pipeline 的增量构建
2. 增加 `incrementalInProgress` 元数据标记到数据库（参考 竞品 `run-analyze.ts:288`）
   - 增量构建开始前设置 `incrementalInProgress = { startedAt, toWriteCount }`
   - 构建成功后清除
   - 崩溃恢复时检测此标记，自动触发全量重建
   - axons 应将 flag 存到 DB metadata 表中（而非单独文件），确保与 DB 状态原子一致
3. 增加 `lastCommit` 快速路径：`git rev-parse HEAD` 检查 commit hash，配合 `git diff --name-only` 排除 `.axons/` 自身变更，实现 up-to-date 零构建返回
4. Watch 模式下的增量构建结果通过 SSE 实时推送（`build_progress` + delta 更新）

5. 增量构建写入集扩展（参考 竞品 三层组合）：
   - BFS 扩展 importers（深度 ≤ 4 层）
   - Shadow-seed：新增文件的模块路径"窃取"已有文件解析
   - 1-hop 边界展开：拉入 unchanged-side 文件
   - 删除集和写入集必须使用相同的 expanded set

#### 2.6 构建过程 delta 推送

**现状**：axons 已有 `mergeGraphDelta` 机制（`graph-adapter.ts`）和 `BuildCompleteEvent` 的 `changed_file_old_node_ids/edge_ids`，但只在 `build_complete` 时使用。

**优化方案**：

- 增量构建时，每个 stage 完成后推送中间 delta（如 parse 完一批文件就推送新增节点）
- 前端实时 merge，用户看到图谱逐步生长
- 全量构建时，可在 InsertNodes 完成后先推送节点（无边的骨架），BuildEdges 完成后再推送边

#### 2.7 分块并行解析（goroutine Pool + Byte Budget Chunking）

**现状**：axons 的 `ParseFiles` 是顺序解析，对大项目（数千文件）是最大性能瓶颈。

**竞品 做法**：文件按 ~2MB 字节预算分 chunk，每个 chunk 分发给 Worker Pool 并行解析。Parse Cache 基于 chunk 内容 hash，单个文件变更只失效其所在 chunk（1/50 chunk 在 1000 文件项目中），预读并发 overlap I/O 与计算。

**优化方案**：

- 引入 goroutine pool 并行解析，池大小默认 `runtime.NumCPU()`，可通过 `BuildOptions.WorkerPoolSize` 配置
- 按 ~2MB 字节预算将文件分 chunk，每个 chunk 由一个 goroutine 处理
- 复用 竞品 的 chunk 内容 hash 缓存思路：ParseCache 按 chunk hash 存储，增量构建时命中缓存直接回放结果
- 预读 goroutine 提前读取下一 chunk 的文件内容，overlap I/O 与解析计算

```go
// pipeline.go
type ParseOptions struct {
    WorkerPoolSize  int   // goroutine 池大小，默认 NumCPU
    ChunkByteBudget int64 // 每个 chunk 的字节预算，默认 2MB
    PreReadChunks   int   // 预读 chunk 数，默认 1
}
```

#### 2.8 依赖声明式 Pipeline 架构

**现状**：axons 的 `pipeline.go` 硬编码顺序执行（CollectFiles → DetectChanges → ParseFiles → InsertNodes → BuildEdges → RunAnalyses → Finalize），新增 stage 需手动插入正确位置，且无法支持并行 stage 或可选 stage。

**竞品 做法**：每个 Phase 声明 `deps: string[]`，runner 用 Kahn 拓扑排序自动决定执行顺序。新增 Phase 只需创建文件并声明依赖，无需修改 runner。支持 `skipGraphPhases` 等可选跳过逻辑。

**优化方案**：

- 将 `PipelineContext` 的 stage 注册改为声明式：

```go
// stages/registry.go
type PipelineStage struct {
    Name    string
    Deps    []string
    Execute func(ctx context.Context, pctx *PipelineContext) error
}

var AllStages = []PipelineStage{
    {Name: "collect", Deps: []string{}, Execute: CollectFiles},
    {Name: "detect", Deps: []string{"collect"}, Execute: DetectChanges},
    {Name: "parse", Deps: []string{"detect"}, Execute: ParseFiles},
    {Name: "insert", Deps: []string{"parse"}, Execute: InsertNodes},
    {Name: "edges", Deps: []string{"insert"}, Execute: BuildEdges},
    {Name: "analyses", Deps: []string{"edges"}, Execute: RunAnalyses},
    {Name: "finalize", Deps: []string{"analyses"}, Execute: Finalize},
}
```

- Runner 自动拓扑排序，检测循环依赖
- 未来可支持：无依赖的 stage 并行执行（如 `routes` 和 `tools`）、可选 stage（如 `communities`）

---

## 问题三：大图谱降级展示

### 现状

| 能力 | 状态 |
|------|------|
| Level Presets | ✅ 已有（`structure` / `class` / `function` / `full` / `no-calls` / `minimal`） |
| Node Size 自适应缩放 | ✅ 已有（`getScaledNodeSize`） |
| FA2 参数自适应 | ✅ 已有（`getFA2Settings`） |
| Barnes-Hut 优化 | ✅ 已有（`nodeCount > 200` 自动开启） |
| Depth Filter | ❌ 缺失 |
| 社区聚类着色 | ❌ 缺失 |
| 自动降级策略 | ❌ 缺失 |
| LOD 渲染 | ❌ 缺失 |
| 分页/按需加载 | ❌ 缺失 |
| 多布局模式 | ❌ 缺失（仅 Force 布局） |

### 竞品 的做法

1. **多级别 Level Presets**：`structure` / `class` / `function` / `full` / `no-calls` / `minimal`
2. **Node Size 自适应缩放**：`> 50000 → 0.4x`、`> 20000 → 0.5x`、`> 5000 → 0.65x`、`> 1000 → 0.8x`
3. **Edge Size 自适应缩放**：`> 20000 → 0.4x`、`> 5000 → 0.6x`
4. **ForceAtlas2 参数自适应**：gravity / scalingRatio / slowDown / barnesHutTheta 根据节点数动态调整
5. **Barnes-Hut 优化**：`nodeCount > 200` 自动开启
6. **Layout 时限**：按节点数设置不同布局时长（20s–45s）
7. **Depth Filter**（`filterGraphByDepth`）：基于选中节点的 N-hop 邻域过滤
8. **Community-based 聚类**：Leiden 社区检测后，同社区节点初始位置聚类，减少布局收敛时间
9. **三种视图模式**：Force / Tree / Circles，各有优化的布局参数
10. **隐藏元数据节点**：`Community` / `Process` 节点 `size: 0` 默认不可见
11. **多布局模式自适应参数**：
    - **Force 模式**：ForceAtlas2，gravity/scalingRatio/slowDown 按节点数分级（< 500 / 500–2K / 2K–10K / > 10K），布局时长 20–45 秒
    - **Tree 模式**：层次布局，带 layer gravity + boundary resistance，适合浏览层次结构
    - **Circles 模式**：同心环布局（4 环：90/240/420/620px），带径向 band 约束，适合概览

### 优化建议

#### 3.1 Depth Filter（N-hop 邻域过滤）

参考 竞品 的 `filterGraphByDepth`，选中节点后只显示 N-hop 范围内的节点：

```typescript
function filterGraphByDepth(
  graph: Graph,
  selectedNodeId: string | null,
  maxHops: number | null,
  visibleLabels: NodeLabel[],
): void {
  if (maxHops === null || selectedNodeId === null) {
    filterGraphByLabels(graph, visibleLabels);
    return;
  }
  const nodesInRange = getNodesWithinHops(graph, selectedNodeId, maxHops);
  graph.forEachNode((nodeId, attributes) => {
    const isLabelVisible = visibleLabels.includes(attributes.nodeType);
    const isInRange = nodesInRange.has(nodeId);
    graph.setNodeAttribute(nodeId, 'hidden', !isLabelVisible || !isInRange);
  });
}
```

- 默认 `maxHops = null`（显示全部）
- 用户可调节 hop 数（1/2/3/无限）
- 配合选中节点，大幅减少渲染压力

#### 3.2 自动降级策略

根据节点数量自动选择展示级别：

| 节点数 | 策略 | 说明 |
|--------|------|------|
| < 1,000 | 全量渲染 + FA2 全参数 | 无限制 |
| 1,000–5,000 | 缩放节点/边尺寸 + Barnes-Hut | 视觉缩放 |
| 5,000–20,000 | 自动切换到 `class` level + 减少布局迭代 | 隐藏 function/method |
| > 20,000 | 自动切换到 `structure` level + 社区聚合 + 提示搜索浏览 | 仅展示模块/类型 |

- 切换时给用户明确提示（如"项目较大，已切换到结构视图，点击节点可展开详情"）
- 用户可手动覆盖自动降级

#### 3.3 社区检测聚类

后端运行 Leiden 算法计算社区归属，前端基于 community 着色和初始定位：

- 后端：在 `RunAnalyses` stage 中运行 Leiden 社区检测，结果存储到 DB
- API：`/api/graph` 响应中增加每个节点的 `community` 字段
- 前端：
  - 同社区节点使用相同颜色（参考 竞品 的 `COMMUNITY_COLORS` 调色板）
  - **初始布局聚类**（核心优化）：根据 `communityMemberships` Map，用黄金角计算各社区 cluster center，同社区节点初始位置在 cluster center 附近（clusterJitter = sqrt(nodeCount) * 1.5）。这大幅加速 FA2 收敛——同社区节点初始就近，弹簧力无需跨越长距离
  - 社区数量可作为图谱概览指标

#### 3.4 LOD 渲染（Level of Detail）

根据缩放级别动态调整渲染细节：

| 缩放级别 | 节点标签 | 边渲染 | 边类型 |
|----------|----------|--------|--------|
| 远距离 | 隐藏 | 隐藏或简化为直线 | — |
| 中距离 | 显示 Class/Interface 标签 | 曲线，仅 CALLS/IMPORTS | 主要边 |
| 近距离 | 显示全部标签 | 曲线，全类型 | 全部 |

- 基于 Sigma.js 的 `camera.ratio` 判断当前缩放级别
- 远距离时跳过大部分边的渲染，这是性能瓶颈的主要来源

#### 3.5 增量图谱加载（按需 Drill-down）

当前 `/api/graph` 一次性返回全量数据。改为分层加载：

1. **初始加载**：`/api/graph?level=structure` — 仅返回模块/类型级节点
2. **Drill-down**：用户双击某节点 → `/api/graph/drilldown?node_id=xxx` — 返回该节点的子图（如类的方法/调用）
3. **搜索定位**：搜索到某函数 → 自动加载该函数 1-hop 邻域

后端需要增加 drill-down API，支持按节点 ID 获取子图。

#### 3.6 性能监测与自动提示

- 增加 FPS 监测：当平均帧率低于 15fps 持续 3 秒时，弹出提示建议切换低粒度视图
- 检测 WebGL 支持：不支持时降级到 Canvas 2D 或 SVG 渲染
- 参考 竞品 的 `WebGPUFallbackDialog.tsx`

#### 3.7 多布局模式

**现状**：axons 仅支持 ForceAtlas2 力导向布局，大项目布局收敛慢且层次结构不清晰。

**竞品 做法**：支持三种布局模式，每种都有针对节点数的自适应参数：
- **Force 模式**：ForceAtlas2，gravity/scalingRatio/slowDown/barnesHutTheta 按节点数分级（< 500 / 500–2K / 2K–10K / > 10K），布局时长 20–45 秒
- **Tree 模式**：层次布局，带 layer gravity + boundary resistance，适合浏览包/模块/文件的层次结构
- **Circles 模式**：同心环布局（4 环：90/240/420/620px），带径向 band 约束，适合概览整个项目

**优化方案**：

1. 增加 Tree 布局：按 CONTAINS/DEFINES 边构建层次，使用 `graphology-layout-noverlap` 防重叠
2. 增加 Circles 布局：按节点类型分层（Project → Package → Module → File → Function），同层节点均匀分布在同心环上
3. ForceAtlas2 参数自适应分级（参考 竞品 `getFA2Settings`）：

| 节点数 | gravity | scalingRatio | slowDown | barnesHutTheta |
|--------|---------|-------------|----------|----------------|
| < 500 | 0.8 | 15 | 1 | — |
| 500–2K | 0.5 | 30 | 2 | 0.6 |
| 2K–10K | 0.3 | 60 | 3 | 0.8 |
| > 10K | 0.15 | 100 | 5 | 0.8 |

4. 布局时长自适应（参考 竞品 `getLayoutDuration`）：20s–45s 按节点数分级

---

## 复审补充发现

以下问题在初版方案中遗漏，通过第二轮深度审查发现：

### 补充一：SSE 基础设施已存在但未充分利用

**发现**：axons 已经有完整的 SSE 事件体系（`EventBroker` + `EventBuildProgress` + `useEventStream`），关键缺失只是 Pipeline → EventBroker 的桥接和前端消费 `build_progress` 事件。原方案说"需要新增 WebSocket/SSE 推送"，实际工作量比预估小很多。

**影响**：问题二的核心优化工作量从"中"降为"小"。

### 补充二：DetectChanges 全量读文件 — 性能灾难

**发现**：`detect_changes.go:32-42` 全量构建时对每个文件执行 `os.ReadFile(path)` 计算 hash。对于大型项目（数万文件），在构建开始前就将所有文件读入内存。

**竞品 做法**：文件遍历只收集路径和元信息，解析时才按需读取。hash 计算是流式的。

**优化**：
- 全量构建时跳过 hash 计算，直接标记为 changed（反正都要重新解析）
- 增量构建时改用 mtime+size 快速判断（Tier 1），仅 mtime 变化的文件才读内容（Tier 2）

### 补充三：增量构建内存问题 — ListAllNodes

**发现**：`insert_nodes.go:36` 增量构建时调用 `ctx.Repo.ListAllNodes()` 将所有已有节点加载到内存。对 10万+ 节点项目占用大量内存。

**竞品 做法**：使用 SQL 查询能力，增量时只操作变更子图，不需要全量节点加载。

**优化**：改为按需查询模式，边构建时针对 import/call 目标在 DB 中查询，或引入节点数阈值自动切换内存/DB 模式。

### 补充四：文件大小上限缺失

**发现**：`collect_files.go` 没有文件大小过滤。竞品 默认 512KB 上限，可配置，且有 tree-sitter 最大 buffer 的硬性上限。

**优化**：在 `CollectFiles` 阶段增加 `os.Stat` 检查文件大小，跳过超大文件，防止 OOM。

### 补充五：Watch 模式未与增量构建打通

**发现**：axons 已有 `watcher.go` + `journal.go` + `IncrementalBuilder` 三层检测，但与 API 构建路径没有打通。缺少 `incrementalInProgress` dirty flag，崩溃后可能导致索引不一致。

**优化**：
- 将 Watcher 与 Pipeline 增量构建路径连接
- 增加 `incrementalInProgress` 元数据标记，崩溃恢复自动全量重建
- Watch 下的增量构建结果通过 SSE 实时推送

### 补充六：前端 Delta 更新能力已有但只在完成时使用

**发现**：axons 已有 `mergeGraphDelta` 机制和 `changed_file_old_node_ids/edge_ids` 字段，但只在 `build_complete` 事件中使用。竞品 在构建过程中就推送 delta。

**优化**：增量构建时，每个 stage 完成后推送中间 delta，前端实时 merge，用户看到图谱逐步增长。

---

## 优先级与工作量评估

| 优先级 | 优化项 | 对应问题 | 工作量 | 说明 |
|--------|--------|----------|--------|------|
| **P0** | Pipeline 进度回调 → EventBroker | 二 | 小 | SSE 管道已存在，只需桥接 |
| **P0** | BuildingState 消费 build_progress | 二 | 小 | 替换静态 spinner 为动态进度 |
| **P0** | DetectChanges 全量读文件优化 | 二 | 中 | 当前 O(n) 全文件读取是性能瓶颈 |
| **P0** | 分块并行解析（goroutine Pool + Chunking） | 二 | 大 | axons 与 竞品 最大性能差距，大项目解析可提速 5–10x |
| **P1** | 依赖声明式 Pipeline 架构 | 二 | 中 | 当前硬编码顺序难以扩展，且阻碍并行化 |
| **P1** | 文件大小上限 | 一 | 小 | 防止超大文件 OOM |
| **P1** | 增量构建内存优化（ListAllNodes） | 二 | 中 | 大项目 10万+ 节点内存问题 |
| **P1** | Depth Filter + 自动降级 | 三 | 中 | 大图谱前端防卡死的关键 |
| **P2** | Watch → 增量构建打通 | 二 | 中 | 已有基础设施，需连接 |
| **P2** | 扩展文件筛选黑名单 | 一 | 小 | 补充缺失的忽略规则 |
| **P2** | incrementalInProgress dirty flag | 二 | 小 | 崩溃恢复安全网 |
| **P2** | 构建过程 delta 推送 | 二 | 中 | 让用户实时看到图谱增长 |
| **P2** | 多布局模式（Tree / Circles） | 三 | 中 | Tree/Circles 布局对大项目远比 Force 布局实用 |
| **P2** | ForceAtlas2 参数自适应分级 | 三 | 小 | 按节点数调整 gravity/scalingRatio/slowDown |
| **P3** | `.axonsignore` + `!` 否定覆盖 | 一 | 中 | 用户自定义忽略 |
| **P3** | 社区检测聚类 + 初始布局聚类 | 三 | 大 | 社区着色 + 黄金角聚类初始定位，加速 FA2 收敛 |
| **P3** | 增量图谱按需加载 | 三 | 大 | 大图谱分页/Drill-down 体验 |
| **P3** | 增量写入集三层组合 | 二 | 中 | BFS + Shadow-seed + 1-hop 边界展开 |