/** @module TimelineWindow @description Selects the latest bounded evidence and maps scroll progress to a timeline index. */
export function latestWindow<T>(values:T[],limit:number){
  const count=Math.max(0,Math.floor(limit));
  const shown=values.slice(Math.max(0,values.length-count));
  return {shown,remaining:values.length-shown.length};
}

export function timelineScrollIndex(scrollTop:number,scrollHeight:number,clientHeight:number,count:number){
  if(count<=0)return 0;
  const distance=scrollHeight-clientHeight;
  if(distance<=0)return count-1;
  const progress=Math.max(0,Math.min(1,scrollTop/distance));
  return Math.round(progress*(count-1));
}
