import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { listReports } from "../api/postdareGo";
import type { ReportIssue } from "../api/types";
import { PageHeader } from "../components/PageHeader";
import { Badge, statusTone } from "../components/ui/badge";
import { Card, CardContent } from "../components/ui/card";
import { Table, Td, Th } from "../components/ui/table";
import { formatDate, shortCommit } from "../lib/utils";
import { useAuthStore } from "../store/auth";

function issueSummary(issues: ReportIssue[]) {
  if (!issues.length) return <span className="text-muted">—</span>;
  const counts: Record<string, number> = { high: 0, medium: 0, low: 0 };
  for (const issue of issues) {
    counts[issue.severity] = (counts[issue.severity] ?? 0) + 1;
  }
  return (
    <span className="space-x-2 whitespace-nowrap text-xs">
      {counts.high > 0 && <span className="text-red-500">{counts.high} high</span>}
      {counts.medium > 0 && <span className="text-amber-500">{counts.medium} medium</span>}
      {counts.low > 0 && <span className="text-muted">{counts.low} low</span>}
    </span>
  );
}

export function ReportsPage() {
  const token = useAuthStore((state) => state.token);
  const reports = useQuery({ queryKey: ["reports"], queryFn: () => listReports(token) });

  return (
    <>
      <PageHeader title="Reports" />
      <Card>
        <CardContent className="p-0">
          <div className="hidden overflow-x-auto md:block">
            <Table>
              <thead>
                <tr>
                  <Th>Report</Th>
                  <Th>Project</Th>
                  <Th>Type</Th>
                  <Th>Status</Th>
                  <Th>Issues</Th>
                  <Th>Conclusion</Th>
                  <Th>Commit</Th>
                  <Th>Task</Th>
                  <Th>Created</Th>
                </tr>
              </thead>
              <tbody>
                {(reports.data?.data ?? []).map((report) => (
                  <tr key={report.id} className="hover:bg-surface-2/70">
                    <Td className="font-medium">#{report.id}</Td>
                    <Td>{report.project_name || `Project ${report.project_id}`}</Td>
                    <Td>{report.type}</Td>
                    <Td>
                      <Badge tone={statusTone(report.status)}>{report.status}</Badge>
                    </Td>
                    <Td>{issueSummary(report.issues ?? [])}</Td>
                    <Td className="max-w-64 truncate">{report.conclusion || "—"}</Td>
                    <Td className="font-mono text-xs">{shortCommit(report.commit_id)}</Td>
                    <Td>
                      <Link className="font-medium text-primary hover:underline" to={`/deploy-tasks/${report.task_id}`}>
                        #{report.task_id}
                      </Link>
                    </Td>
                    <Td>{formatDate(report.created_at)}</Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </div>
          <div className="divide-y divide-border md:hidden">
            {(reports.data?.data ?? []).map((report) => (
              <Link key={report.id} to={`/deploy-tasks/${report.task_id}`} className="block p-4 active:bg-surface-2/70">
                <div className="flex items-center justify-between gap-3">
                  <span className="font-medium text-primary">#{report.id}</span>
                  <Badge tone={statusTone(report.status)}>{report.status}</Badge>
                </div>
                <div className="mt-1 truncate text-sm text-ink">
                  {report.project_name || `Project ${report.project_id}`} · {report.type}
                </div>
                <div className="mt-1 truncate text-xs text-muted">{report.conclusion || "—"}</div>
                <div className="mt-1 text-xs text-muted">
                  #{report.task_id} · <span className="font-mono">{shortCommit(report.commit_id)}</span> · {formatDate(report.created_at)}
                </div>
              </Link>
            ))}
          </div>
        </CardContent>
      </Card>
    </>
  );
}
