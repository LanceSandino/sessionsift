/** @module LibraryGroups @description Orders the session library and groups it by how recently each conversation was active or created. */
import {sortOrders,sortSessions,type SessionSortOrder} from "./components/sessionSort";
import type {Session} from "./types";

export type LibraryGroup={label:string;sessions:Session[]};

const dayMs=86400000;

function startOfDay(time:number){
  const date=new Date(time);
  date.setHours(0,0,0,0);
  return date.getTime();
}

export function recencyLabel(value:string,now:number):string{
  const time=Date.parse(value);
  if(!Number.isFinite(time))return "Undated";
  const today=startOfDay(now);
  if(time>=today)return "Today";
  if(time>=today-dayMs)return "Yesterday";
  if(time>=today-6*dayMs)return "This week";
  return "Earlier";
}

/** The time a library row reports: creation for creation orders, otherwise the latest update. */
export function libraryTime(session:Session,order:SessionSortOrder):string{
  return order==="started_desc"||order==="started_asc"?session.started_at:session.ended_at||session.started_at;
}

/** Keeps the incoming order inside each group; groups appear as they are first met. */
export function groupByRecency(sessions:Session[],now:number,timeOf:(session:Session)=>string=session=>session.ended_at||session.started_at):LibraryGroup[]{
  const groups:LibraryGroup[]=[];
  for(const session of sessions){
    const label=recencyLabel(timeOf(session),now);
    const group=groups.find(value=>value.label===label);
    if(group)group.sessions.push(session);
    else groups.push({label,sessions:[session]});
  }
  return groups;
}

/** Date orders group by recency of the time they sort on; title and token orders are one ranked list. */
export function libraryGroups(sessions:Session[],order:SessionSortOrder,now:number):LibraryGroup[]{
  const sorted=sortSessions(sessions,order);
  if(order==="title_asc"||order==="tokens_desc")return sorted.length?[{label:sortOrders.find(item=>item.id===order)!.label,sessions:sorted}]:[];
  return groupByRecency(sorted,now,session=>libraryTime(session,order));
}
