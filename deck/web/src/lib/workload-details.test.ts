import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import WorkloadDetails from './components/WorkloadDetails.svelte';
import TaskDrawer from './components/TaskDrawer.svelte';
import { evidenceDownloadURL } from './evidence';
import type { Task, AppEvent, TaskDetail, EvidenceRecord, Decision } from './types';
const task: Task = { id:'task', project_id:'project', title:'Change', description:'', acceptance_criteria:'A test passes', status:'blocked', priority:'medium', budget_minutes:5, version:1, created_at:1, updated_at:1, budget:{ time_minutes:5,max_tokens:20000,max_attempts:2,max_cost_minor:10,currency:'USD',max_concurrency:1,authority:['source.read','source.write','pull_request.create'] }, workload:{ id:'repository/v1', digest:'a'.repeat(64), harness:'codex/v1',authority:[],max_tokens:20000,budget_minutes:5,min_attempts:2,tools:[],context_kinds:[] } };
const proof: EvidenceRecord = {id:'proof',task_id:'task',run_id:'run',plan_revision_id:'plan',type:'test',producer:'marina',verifier:'marina/verification',subject_digest:'b'.repeat(40),source_revision:'b'.repeat(40),command:'tests passed',outcome:'passed',provenance_uri:'',provenance_digest:'proof',observed_at:1,artifact_uri:'marina:factory/runs/run/artifacts/sha256:'+'c'.repeat(64),artifact_digest:'sha256:'+'c'.repeat(64),admission_status:'current',created_at:1};
const drawer = (detail: Partial<TaskDetail>) => render(TaskDrawer,{props:{detail:{
  task:{...task,status:'done'}, runs:[], plan_revisions:[],events:[],decisions:[],artifacts:[],evidence:[],commands:[],operational_states:[],trail:[],display_state:'completed',
  provider_status:{project_id:'project',connectivity:'connected',sync_state:'current',sync_lag_millis:0,lease_state:'none',degraded_dependencies:[],stale_projection_count:0,rejected_projection_count:0},
  ...detail
} as TaskDetail,onclose:()=>{},onrefresh:async()=>{},onmove:async()=>{},onsave:async()=>{},onanswer:async()=>{},onretention:async()=>{}}}).body;
describe('workload inspection', () => {
  it('shows a repository change and uncertain usage', () => {
    const events = [{ id:2, created_at:1, run_id:'run', type:'model.usage', message:'Marina reported model usage', payload:{ reported_tokens:40, reported_cost_micros:100, pending_model_calls:1, reserved_tokens:1200, reserved_cost_micros:1000, currency:'USD' } }] as AppEvent[];
    const html=render(WorkloadDetails,{props:{task,events,runID:'run'}}).body;
    expect(html).toContain('Repository change');expect(html).toContain('Usage awaiting confirmation');expect(html).toContain('1,200');
  });
  it('does not reuse usage from an earlier run', () => {
    const events=[{ id:1,created_at:1,type:'model.usage',run_id:'old',message:'Old result',payload:{reported_tokens:99999} }] as AppEvent[];
    const html=render(WorkloadDetails,{props:{task,events,runID:'new'}}).body;
    expect(html).not.toContain('99,999');expect(html).toContain('No usage reported yet');
  });
  it('links the pull request Marina reported', () => {
    const html=drawer({runs:[{id:'run',task_id:'task',agent_id:'marina',version:1,fence_token:1,task_version:1,acceptance_criteria_digest:'criteria',status:'completed',plan:'',result_summary:'Marina run run succeeded with pull request https://github.com/o/r/pull/7',started_at:1,updated_at:2}]});
    expect(html).toContain('href="https://github.com/o/r/pull/7"');expect(html).toContain('Review pull request');
  });
  it('asks for a decision without claiming the agent paused', () => {
    const decision: Decision = {id:'d',task_id:'task',question:'Approve plan revision 1?',options:['Approve plan','Request changes'],recommendation:'Approve plan',risk_class:'medium',evidence_summary:'',estimated_cost_minor:0,currency:'USD',waiting_consequence:'Execution waits.',rejecting_consequence:'',status:'pending',answer:'',created_at:1};
    const html=drawer({task:{...task,status:'waiting_decision'},decisions:[decision]});
    expect(html).toContain('Approve plan revision 1?');expect(html).not.toContain('Agent is paused');
  });
});
describe('evidence downloads', () => {
  it('offers a download only for files Marina retains', () => {
    expect(evidenceDownloadURL('task 1',proof)).toBe('/api/v1/operator/tasks/task%201/artifacts/sha256%3A'+'c'.repeat(64));
    expect(evidenceDownloadURL('task',{...proof,artifact_uri:'https://marina.internal/artifacts/old'})).toBeUndefined();
    expect(evidenceDownloadURL('task',{...proof,artifact_uri:'',artifact_digest:''})).toBeUndefined();
  });
});
