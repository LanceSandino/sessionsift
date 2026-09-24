/** @module SessionFilters @description Pure conversation filtering for the responsive session rail. */
import type {Session} from "../types";

export type SessionFilters={
  query:string;
  /** A source ID, "chat" for every exported chat product, or "" for all. */
  source:string;
  repo:string;
  model:string;
  date:string;
  highTokens:boolean;
  failures:boolean;
  findings:boolean;
  internal:boolean;
};

export function filterSessions(sessions:Session[],filters:SessionFilters):Session[]{
  const normalizedQuery=filters.query.trim().toLowerCase();
  return sessions.filter(session=>{
    const metrics=session.metrics;
    const source=session.source||"codex";
    const searchable=`${session.title} ${session.repo_display_name??""}`.toLowerCase();

    if(!filters.internal&&session.session_kind==="internal_review")return false;
    if(filters.source==="chat"){if(source!=="chatgpt"&&source!=="claude")return false}
    else if(filters.source&&source!==filters.source)return false;
    if(normalizedQuery&&!searchable.includes(normalizedQuery))return false;
    if(filters.repo&&session.repo_display_name!==filters.repo)return false;
    if(filters.model&&session.model!==filters.model)return false;
    if(filters.date&&session.started_at.slice(0,10)!==filters.date)return false;
    if(filters.highTokens&&(metrics?.input_tokens||0)<100000)return false;
    if(filters.failures&&(metrics?.failed_tool_calls||0)===0)return false;
    if(filters.findings&&(metrics?.finding_count||0)===0)return false;
    return true;
  });
}
