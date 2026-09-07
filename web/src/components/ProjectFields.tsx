import { useState } from "react";
import { ArrowDown, ArrowUp, HelpCircle, Plus, Trash2 } from "lucide-react";

import type { OutboundWebhookTemplate, Project, ProjectStage, ProjectStageRunWhen, ProjectStageType } from "../api/types";
import { Input } from "./ui/input";
import { Textarea } from "./ui/textarea";

const defaultMessageTemplate = `Postdare Go {{ .Scene }}
项目: {{ .Project.Name }}
Git: {{ .Task.GitProvider }}
触发: {{ .Task.TriggerType }}
分支: {{ .Task.Branch }}
commit: {{ .Task.CommitID }}
消息: {{ .Task.CommitMessage }}
阶段: {{ .Task.CurrentStage }}
状态: {{ .Task.Status }}
失败原因: {{ .Task.FailReason }}
任务ID: {{ .Task.ID }}
耗时: {{ .Duration }}`;

const reportCaptureContract = `{
  "report_type": "ai_review",
  "status": "success | failed | skipped",
  "conclusion": "必填，一行结论",
  "summary": "摘要，建议 240 字以内",
  "issues": [
    {
      "severity": "high | medium | low",
      "title": "必填",
      "location": "文件:行号",
      "trigger": "触发条件",
      "impact": "影响",
      "suggestion": "建议"
    }
  ],
  "markdown": "完整 Markdown 报告",
  "error_message": ""
}`;

type Props = {
  value: Partial<Project>;
  onChange: (value: Partial<Project>) => void;
};

export function ProjectFields({ value, onChange }: Props) {
  const set = (key: keyof Project, next: string | boolean | ProjectStage[]) => onChange({ ...value, [key]: next });
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Field label="Name">
        <Input value={value.name ?? ""} onChange={(e) => set("name", e.target.value)} />
      </Field>
      <Field label="Project key">
        <Input value={value.project_key ?? ""} onChange={(e) => set("project_key", e.target.value)} />
      </Field>
      <Field label="Git provider">
        <select
          className="h-9 w-full rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/30"
          value={value.git_provider ?? "gitee"}
          onChange={(e) => set("git_provider", e.target.value)}
        >
          <option value="gitee">Gitee</option>
          <option value="github">GitHub</option>
        </select>
      </Field>
      <Field label="Branch">
        <Input value={value.branch ?? "main"} onChange={(e) => set("branch", e.target.value)} />
      </Field>

      <Field label="App directory">
        <Input value={value.app_dir ?? ""} onChange={(e) => set("app_dir", e.target.value)} />
      </Field>
      <Field label="App log path">
        <Input value={value.app_log_path ?? ""} onChange={(e) => set("app_log_path", e.target.value)} />
      </Field>
      <Field label="Webhook secret">
        <Input value={value.webhook_secret ?? ""} onChange={(e) => set("webhook_secret", e.target.value)} />
      </Field>
      <div className="lg:col-span-2">
        <StageEditor stages={value.deploy_stages ?? []} onChange={(next) => set("deploy_stages", next)} />
      </div>
      <div className="lg:col-span-2">
        <Field label="Rollback command">
          <Textarea value={value.rollback_cmd ?? ""} onChange={(e) => set("rollback_cmd", e.target.value)} />
        </Field>
      </div>
      <label className="flex items-center gap-2 text-sm text-ink">
        <input
          type="checkbox"
          checked={Boolean(value.auto_deploy_enabled)}
          onChange={(e) => set("auto_deploy_enabled", e.target.checked)}
          className="h-4 w-4 accent-primary"
        />
        Auto deploy
      </label>
    </div>
  );
}

function StageEditor({ stages, onChange }: { stages: ProjectStage[]; onChange: (next: ProjectStage[]) => void }) {
  const [openHints, setOpenHints] = useState<Record<number, boolean>>({});
  const toggleHint = (index: number) => setOpenHints((prev) => ({ ...prev, [index]: !prev[index] }));
  const update = (index: number, patch: Partial<ProjectStage>) =>
    onChange(stages.map((stage, i) => (i === index ? ({ ...stage, ...patch } as ProjectStage) : stage)));
  const updateConfig = (index: number, patch: Record<string, string>) =>
    onChange(
      stages.map((stage, i) =>
        i === index ? ({ ...stage, config: { ...(stage.config ?? {}), ...patch } } as ProjectStage) : stage
      )
    );
  const remove = (index: number) => onChange(stages.filter((_, i) => i !== index));
  const move = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= stages.length) return;
    const next = [...stages];
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next);
  };
  const add = () => onChange([...stages, newStage("command")]);
  const setType = (index: number, type: ProjectStageType) => {
    const current = stages[index];
    onChange(stages.map((stage, i) => (i === index ? { ...newStage(type), name: current.name, enabled: current.enabled } : stage)));
  };

  return (
    <div className="grid gap-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted">Deploy stages</span>
        <button
          type="button"
          onClick={add}
          className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs text-ink hover:bg-muted/10"
        >
          <Plus className="h-3.5 w-3.5" />
          Add stage
        </button>
      </div>
      {stages.length === 0 ? (
        <p className="rounded-md border border-dashed border-border px-3 py-4 text-center text-xs text-muted">
          No stages yet. Stages run top to bottom during a deploy. Add one to get started.
        </p>
      ) : (
        <ol className="grid gap-3">
          {stages.map((stage, index) => (
            <li key={index} className="grid gap-2 rounded-md border border-border p-3">
              <div className="flex items-start gap-2">
                <span className="mt-2 w-5 shrink-0 text-center text-xs text-muted">{index + 1}</span>
                <div className="grid flex-1 gap-2">
                  <Input
                    placeholder="Stage name (e.g. build)"
                    value={stage.name}
                    onChange={(e) => update(index, { name: e.target.value })}
                  />
                  <div className="grid gap-2 md:grid-cols-2">
                    <select
                      className="h-9 w-full rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/30"
                      value={stage.type}
                      onChange={(e) => setType(index, e.target.value as ProjectStageType)}
                    >
                      <option value="command">Command</option>
                      <option value="health_check">Health check</option>
                      <option value="outbound_webhook">Outbound webhook</option>
                    </select>
                    <select
                      className="h-9 w-full rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/30"
                      value={stage.run_when ?? ""}
                      onChange={(e) => update(index, { run_when: (e.target.value || undefined) as ProjectStageRunWhen | undefined })}
                    >
                      <option value="">Main flow</option>
                      <option value="success">On success</option>
                      <option value="failed">On failed</option>
                      <option value="always">Always</option>
                    </select>
                  </div>
                  {stage.type === "command" ? (
                    <div className="grid gap-2">
                      <Textarea
                        placeholder="Shell command"
                        value={stage.config.command}
                        onChange={(e) => updateConfig(index, { command: e.target.value })}
                      />
                      <div className="flex items-start gap-1.5 text-xs text-ink">
                        <input
                          type="checkbox"
                          checked={Boolean(stage.config.capture_as)}
                          onChange={(e) => updateConfig(index, { capture_as: e.target.checked ? "ai_review" : "" })}
                          className="mt-0.5 h-4 w-4 shrink-0 accent-primary"
                          id={`capture-${index}`}
                        />
                        <label htmlFor={`capture-${index}`} className="grid gap-0.5">
                          <span>Capture stdout as report</span>
                          <span className="text-muted">
                            该阶段的 stdout 不再写入部署日志，命令必须打印下方结构的 JSON。
                          </span>
                        </label>
                        <button
                          type="button"
                          title="报告 JSON 结构"
                          aria-label="报告 JSON 结构"
                          aria-pressed={Boolean(openHints[index])}
                          onClick={() => toggleHint(index)}
                          className={`ml-auto inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md border hover:bg-muted/10 ${
                            openHints[index] ? "border-primary text-primary" : "border-border text-muted"
                          }`}
                        >
                          <HelpCircle className="h-3.5 w-3.5" />
                        </button>
                      </div>
                      {openHints[index] ? (
                        <pre className="max-h-56 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-surface p-3 text-xs leading-relaxed text-muted">
                          {reportCaptureContract}
                        </pre>
                      ) : null}
                    </div>
                  ) : null}
                  {stage.type === "health_check" ? (
                    <Input
                      placeholder="Health URL"
                      value={stage.config?.url ?? ""}
                      onChange={(e) => updateConfig(index, { url: e.target.value })}
                    />
                  ) : null}
                  {stage.type === "outbound_webhook" ? (
                    <div className="grid gap-2">
                      <Input
                        placeholder="Webhook URL"
                        value={stage.config?.url ?? ""}
                        onChange={(e) => updateConfig(index, { url: e.target.value })}
                      />
                      <select
                        className="h-9 w-full rounded-md border border-border bg-background px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/30"
                        value={stage.config?.template ?? "dingtalk_text"}
                        onChange={(e) => updateConfig(index, { template: e.target.value as OutboundWebhookTemplate })}
                      >
                        <option value="dingtalk_text">DingTalk text</option>
                        <option value="wecom_text">WeCom text</option>
                        <option value="feishu_text">Feishu text</option>
                        <option value="feishu_report_card">Feishu report card</option>
                        <option value="generic_json">Generic JSON</option>
                      </select>
                      <div className="relative">
                        <Textarea
                          className="min-h-[80px] w-full"
                          placeholder="留空则使用系统默认消息模板"
                          value={stage.config?.message_template ?? ""}
                          onChange={(e) => updateConfig(index, { message_template: e.target.value })}
                        />
                        <button
                          type="button"
                          title="默认消息模板"
                          aria-label="默认消息模板"
                          aria-pressed={Boolean(openHints[index])}
                          onClick={() => toggleHint(index)}
                          className={`absolute -right-9 top-1 inline-flex h-7 w-7 items-center justify-center rounded-md border hover:bg-muted/10 ${
                            openHints[index] ? "border-primary text-primary" : "border-border text-muted"
                          }`}
                        >
                          <HelpCircle className="h-3.5 w-3.5" />
                        </button>
                      </div>
                      {openHints[index] ? (
                        <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-surface p-3 text-xs leading-relaxed text-muted">
                          {defaultMessageTemplate}
                        </pre>
                      ) : null}
                    </div>
                  ) : null}
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-ink">
                    <label className="flex items-center gap-1.5">
                      <input
                        type="checkbox"
                        checked={stage.enabled}
                        onChange={(e) => update(index, { enabled: e.target.checked })}
                        className="h-4 w-4 accent-primary"
                      />
                      Enabled
                    </label>
                    <label className="flex items-center gap-1.5">
                      <input
                        type="checkbox"
                        checked={Boolean(stage.continue_on_error)}
                        onChange={(e) => update(index, { continue_on_error: e.target.checked })}
                        className="h-4 w-4 accent-primary"
                      />
                      Continue on error
                    </label>
                  </div>
                </div>
                <div className="flex shrink-0 flex-col gap-1">
                  <IconButton label="Move up" disabled={index === 0} onClick={() => move(index, -1)}>
                    <ArrowUp className="h-3.5 w-3.5" />
                  </IconButton>
                  <IconButton label="Move down" disabled={index === stages.length - 1} onClick={() => move(index, 1)}>
                    <ArrowDown className="h-3.5 w-3.5" />
                  </IconButton>
                  <IconButton label="Remove stage" onClick={() => remove(index)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </IconButton>
                </div>
              </div>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function newStage(type: ProjectStageType): ProjectStage {
  if (type === "health_check") {
    return { name: "health_check", type, enabled: true, config: {} };
  }
  if (type === "outbound_webhook") {
    return {
      name: "outbound_webhook",
      type,
      enabled: true,
      run_when: "always",
      continue_on_error: true,
      config: { template: "dingtalk_text" }
    };
  }
  return { name: "", type: "command", enabled: true, config: { command: "" } };
}

function IconButton({
  children,
  label,
  onClick,
  disabled
}: {
  children: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      disabled={disabled}
      className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted hover:bg-muted/10 disabled:cursor-not-allowed disabled:opacity-40"
    >
      {children}
    </button>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="grid gap-1.5 text-sm">
      <span className="text-xs font-medium text-muted">{label}</span>
      {children}
    </label>
  );
}
