/** @module SessionRevision @description Builds a lightweight change key that gates expensive session-detail refreshes. */
import type {Session} from "./types";

export function sessionRevision(session?:Session):string{
  if(!session)return "";
  // Session detail payloads can be several megabytes. Refresh them when a
  // prompt completes, not for every tool call emitted by an in-flight prompt.
  return [session.ended_at,session.metrics?.turn_count,session.status].join(":");
}
