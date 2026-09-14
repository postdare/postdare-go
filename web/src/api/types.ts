export type GitProvider = "gitee" | "github";
export type DeployStatus = "pending" | "running" | "success" | "failed" | "canceled" | "rollbacked";
export type ProjectStageType = "command" | "health_check" | "outbound_webhook";
export type ProjectStageRunWhen = "success" | "failed" | "always";
export type OutboundWebhookTemplate = "dingtalk_text" | "wecom_text" | "feishu_text" | "feishu_report_card" | "generic_json";

export interface User {
  id: number;
  username: string;
  role: string;
  must_change_password: boolean;
}

export interface ProjectStageBase {
  name: string;
  type: ProjectStageType;
  enabled: boolean;
  run_when?: ProjectStageRunWhen;
  continue_on_error?: boolean;
}

export interface CommandProjectStage extends ProjectStageBase {
  type: "command";
  config: {
    command: string;
    // The report type this stage captures. "report" is the legacy spelling of
    // "ai_review" and is still accepted by the server.
    capture_as?: "ai_review" | "report" | "";
  };
}

export interface HealthCheckProjectStage extends ProjectStageBase {
  type: "health_check";
  config?: {
    url?: string;
  };
}

export interface OutboundWebhookProjectStage extends ProjectStageBase {
  type: "outbound_webhook";
  config?: {
    url?: string;
    template?: OutboundWebhookTemplate;
    message_template?: string;
  };
}

export type ProjectStage = CommandProjectStage | HealthCheckProjectStage | OutboundWebhookProjectStage;

export interface Project {
  id: number;
  name: string;
  project_key: string;
  git_provider: GitProvider;
  branch: string;
  app_dir: string;
  rollback_cmd?: string;
  deploy_stages?: ProjectStage[];
  app_log_path?: string;
  webhook_secret?: string;
  auto_deploy_enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface DeployTask {
  id: number;
  project_id: number;
  project?: Project;
  trigger_type: string;
  git_provider?: GitProvider;
  branch?: string;
  commit_id?: string;
  commit_message?: string;
  commit_author?: string;
  status: DeployStatus;
  current_stage?: string;
  fail_reason?: string;
  log_file?: string;
  started_at?: string | null;
  finished_at?: string | null;
  created_at?: string;
  updated_at?: string;
  stages?: DeployTaskStage[];
}

export interface DeployTaskStage {
  id: number;
  task_id: number;
  name: string;
  status: string;
  started_at?: string | null;
  finished_at?: string | null;
  exit_code?: number | null;
  error_message?: string;
}

export interface WebhookEvent {
  id: number;
  provider: GitProvider;
  project_id?: number;
  project_key?: string;
  event_type?: string;
  branch?: string;
  commit_id?: string;
  before_commit_id?: string;
  commit_message?: string;
  commit_author?: string;
  delivery_id?: string;
  signature_valid: boolean;
  handled: boolean;
  ignored_reason?: string;
  created_at?: string;
}

export interface ReportIssue {
  severity: "high" | "medium" | "low";
  title: string;
  location?: string;
  trigger?: string;
  impact?: string;
  suggestion?: string;
  // Excerpt of the reviewed diff, cut from the real diff by the capture script,
  // so a finding can be checked without opening the repo. Served on the shared
  // report too, which is what makes it checkable for a reader without access.
  diff_hunk?: string;
}

export interface Report {
  id: number;
  type: string;
  project_id: number;
  project_name: string;
  task_id: number;
  trigger_type: string;
  branch: string;
  commit_id: string;
  before_commit_id: string;
  deploy_status: string;
  status: "success" | "failed" | "skipped";
  conclusion: string;
  summary: string;
  issues: ReportIssue[];
  markdown: string;
  error_message?: string;
  share_enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface DashboardSummary {
  project_total: number;
  today_deploy_total: number;
  today_success_total: number;
  today_failed_total: number;
  success_rate: number;
  recent_failed_tasks: DeployTask[];
}

export interface ListResponse<T> {
  data: T[];
  pagination: {
    page: number;
    page_size: number;
    total: number;
  };
  request_id?: string;
}

export interface DataResponse<T> {
  data: T;
  request_id?: string;
}

export type IssueStatus = "backlog" | "todo" | "in_progress" | "done" | "canceled";
export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export interface Board {
  id: number;
  name: string;
  key: string;
  description?: string;
  project_id?: number | null;
  project_name?: string;
  issue_counts: Record<IssueStatus, number>;
  created_at?: string;
  updated_at?: string;
}

export interface Issue {
  id: number;
  board_id: number;
  number: number;
  /** The board-scoped name, e.g. ENG-42 — what people type in a commit message. */
  identifier: string;
  board_key: string;
  title: string;
  description?: string;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_id?: number | null;
  assignee_name?: string;
  creator_id?: number | null;
  creator_name?: string;
  labels: string[];
  position: string;
  completed_at?: string | null;
  /** How many comments the issue's thread holds, so a card can show that a
   *  conversation is happening on it without fetching the thread. */
  comment_count?: number;
  created_at?: string;
  updated_at?: string;
}

export interface IssueComment {
  id: number;
  issue_id: number;
  author_id?: number | null;
  author_name?: string;
  /** Markdown, in the same dialect the description uses. */
  body: string;
  /** The body was rewritten after it was posted. */
  edited: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface BoardUser {
  id: number;
  username: string;
  role: string;
}

export interface IssueMetadata {
  statuses: IssueStatus[];
  priorities: IssuePriority[];
}
