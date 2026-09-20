/** @module Format @description Formats local metrics without changing their meaning. */
export function compact(value:number){return Intl.NumberFormat("en",{notation:"compact",maximumFractionDigits:1}).format(value)}
export function percent(value:number){return `${Math.round(value*100)}%`}
export function duration(ms:number){if(!ms||ms<0)return "—";const mins=Math.round(ms/60000);return mins<60?`${mins}m`:`${Math.floor(mins/60)}h ${mins%60}m`}
export function elapsed(start:string,current:string){const ms=new Date(current).getTime()-new Date(start).getTime();const mins=Math.max(0,Math.round(ms/60000));return `${String(Math.floor(mins/60)).padStart(2,"0")}:${String(mins%60).padStart(2,"0")}`}
