/** @module OptimizationBehavior @description Provides deterministic formatting and update payloads for the local optimization workflow. */
import type {OptimizationExperiment,RecommendationState} from "./types";

export const recommendationStates:readonly RecommendationState[]=["proposed","accepted","applied","dismissed","measured"];

export function formatOptimizationDelta(value:number,unit:string){
  const rounded=Number.isInteger(value)?String(value):String(Number(value.toFixed(2)));
  return `${value>0?"+":""}${rounded} ${unit}`;
}

export function measurementUpdate(experiment:OptimizationExperiment,followup:string){
  return {id:experiment.id,title:experiment.title,hypothesis:experiment.hypothesis,recommendation_id:experiment.recommendation_id,baseline_session_id:experiment.baseline_session_id,followup_session_id:followup};
}
