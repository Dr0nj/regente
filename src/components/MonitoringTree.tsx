import { useState } from "react";

export interface TreeNode {
  id: string;
  label: string;
  status: string;
}

export interface TreeTeam {
  name: string;
  jobs: TreeNode[];
}

interface MonitoringTreeProps {
  teams: TreeTeam[];
  selectedJobId?: string | null;
  onSelectJob?: (jobId: string) => void;
}

const STATUS_DOT: Record<string, string> = {
  RUNNING: "bg-cyan-400 animate-pulse",
  SUCCESS: "bg-emerald-400",
  FAILED: "bg-red-400",
  WAITING: "bg-amber-400",
  INACTIVE: "bg-slate-500",
};

export default function MonitoringTree({ teams, selectedJobId, onSelectJob }: MonitoringTreeProps) {
  const [openTeams, setOpenTeams] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(teams.map((t) => [t.name, true]))
  );

  const toggleTeam = (name: string) => {
    setOpenTeams((prev) => ({ ...prev, [name]: !prev[name] }));
  };

  return (
    <div className="pt-1 pb-2">
      <p className="text-[10px] font-bold uppercase tracking-[0.15em] text-slate-500 mb-2 px-1">
        Scheduling Tree
      </p>
      <ul className="space-y-1">
        {teams.map((team) => (
          <li key={team.name}>
            <button
              className="flex items-center gap-1.5 w-full text-left py-1 px-1 rounded-md hover:bg-white/[0.03] transition-colors"
              onClick={() => toggleTeam(team.name)}
            >
              <svg
                className={`shrink-0 transition-transform duration-150 ${openTeams[team.name] ? "rotate-90" : ""}`}
                width="10" height="10" viewBox="0 0 20 20" fill="none"
              >
                <path d="M7 5l5 5-5 5" stroke="#94a3b8" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <span className="text-[11px] font-semibold text-slate-300 tracking-wide">{team.name}</span>
              <span className="ml-auto text-[9px] text-slate-500 tabular-nums">{team.jobs.length}</span>
            </button>
            {openTeams[team.name] && (
              <ul className="ml-4 mt-0.5 space-y-px border-l border-white/[0.06] pl-2">
                {team.jobs.map((job) => (
                  <li key={job.id}>
                    <button
                      className={`flex items-center gap-2 w-full text-left py-1 px-1.5 rounded-md transition-colors text-[11px] ${
                        selectedJobId === job.id
                          ? "bg-cyan-500/10 text-cyan-300"
                          : "text-slate-400 hover:text-slate-200 hover:bg-white/[0.03]"
                      }`}
                      onClick={() => onSelectJob?.(job.id)}
                    >
                      <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${STATUS_DOT[job.status] ?? STATUS_DOT.INACTIVE}`} />
                      <span className="truncate">{job.label}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
