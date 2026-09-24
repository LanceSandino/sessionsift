/** @module SessionSort @description Orders conversations without mutating API results. */
import type {Session} from "../types";

export type SessionSortOrder="updated_desc"|"updated_asc"|"started_desc"|"started_asc"|"title_asc"|"tokens_desc";

export const sortOrders:readonly {id:SessionSortOrder;label:string}[]=[
  {id:"updated_desc",label:"Recently updated"},
  {id:"updated_asc",label:"Least recently updated"},
  {id:"started_desc",label:"Newest created"},
  {id:"started_asc",label:"Oldest created"},
  {id:"title_asc",label:"Title A–Z"},
  {id:"tokens_desc",label:"Most input tokens"},
];

const timestamp=(value:string)=>Number.isFinite(Date.parse(value))?Date.parse(value):0;
const tieBreak=(a:Session,b:Session)=>a.title.localeCompare(b.title)||a.id.localeCompare(b.id);

export function sortSessions(sessions:Session[],order:SessionSortOrder):Session[]{
  return [...sessions].sort((a,b)=>{
    switch(order){
      case "updated_desc":return timestamp(b.ended_at)-timestamp(a.ended_at)||timestamp(b.started_at)-timestamp(a.started_at)||tieBreak(a,b);
      case "updated_asc":return timestamp(a.ended_at)-timestamp(b.ended_at)||timestamp(a.started_at)-timestamp(b.started_at)||tieBreak(a,b);
      case "started_desc":return timestamp(b.started_at)-timestamp(a.started_at)||tieBreak(a,b);
      case "started_asc":return timestamp(a.started_at)-timestamp(b.started_at)||tieBreak(a,b);
      case "title_asc":return tieBreak(a,b);
      case "tokens_desc":return (b.metrics?.input_tokens||0)-(a.metrics?.input_tokens||0)||tieBreak(a,b);
    }
  });
}
