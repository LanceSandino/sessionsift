/** @module ReviewState @description Interprets append-only review records and note targets as human decisions, never as proof of correctness. */
import type {Annotation,Review,Turn} from "./types";

export type DecisionKind="pending"|"reviewed"|"followup"|"commented"|"other";
export type Decision={kind:DecisionKind;label:string;review?:Review};

// "approved" is the status earlier builds and hosted reviewers recorded.
const kinds:Record<string,Exclude<DecisionKind,"pending"|"other">>={reviewed:"reviewed",approved:"reviewed",changes_requested:"followup",needs_follow_up:"followup",commented:"commented"};
const labels:Record<DecisionKind,string>={pending:"Ready for review",reviewed:"Reviewed",followup:"Needs follow-up",commented:"Commented",other:"Decision recorded"};

export function decisionKind(status:string):Exclude<DecisionKind,"pending">{
  return kinds[status.trim().toLowerCase()]||"other";
}

export function decisionLabel(status:string):string{
  const kind=decisionKind(status);
  return kind==="other"?status:labels[kind];
}

/** Decisions are appended, so the newest one is the session's current state. */
export function currentDecision(reviews:Review[]):Decision{
  const review=reviews.slice().sort((a,b)=>b.created_at.localeCompare(a.created_at))[0];
  if(!review)return {kind:"pending",label:labels.pending};
  const kind=decisionKind(review.status);
  return {kind,label:kind==="other"?review.status:labels[kind],review};
}

export function noteTarget(annotation:Pick<Annotation,"turn_id">,turns:Pick<Turn,"id"|"sequence">[]):string{
  if(!annotation.turn_id)return "Entire session";
  const turn=turns.find(value=>value.id===annotation.turn_id);
  return turn?`Prompt ${turn.sequence}`:"A prompt no longer in this session";
}

export function newestFirst<T extends {created_at:string}>(values:T[]):T[]{
  return values.slice().sort((a,b)=>b.created_at.localeCompare(a.created_at));
}
