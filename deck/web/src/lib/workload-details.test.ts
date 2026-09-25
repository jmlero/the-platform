import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import WorkloadDetails from './components/WorkloadDetails.svelte';
import TaskDrawer from './components/TaskDrawer.svelte';
import type { Task, AppEvent, TaskDetail, EvidenceRecord } from './types';
const task: Task = { id:'task', project_id:'project', title:'Analysis', description:'', acceptance_criteria:'Valid report', status:'blocked', priority:'medium', budget_minutes:5, version:1, created_at:1, updated_at:1, budget:{ time_minutes:5,max_tokens:20000,max_attempts:1,max_cost_minor:10,currency:'USD',max_concurrency:1,authority:[] }, workload:{ id:'diagnostic-analysis/v1', digest:'a'.repeat(64), harness:'langchain-create-agent/v1',authority:[],max_tokens:20000,budget_minutes:5,min_attempts:1,tools:[],context_kinds:[] } };
describe('workload inspection', () => {
  it('shows analysis identity and uncertain usage without claiming a PR result', () => {
    const events = [{ id:2, created_at:1, run_id:'run', type:'execution.progress', message:'Provider unavailable', payload:{ workload:'diagnostic-analysis/v1', profile_digest:'a'.repeat(64), harness:'langchain-create-agent/v1', execution_id:'exec', reported_tokens:40, reported_cost_micros:100, pending_model_calls:1, reserved_tokens:1200, reserved_cost_micros:1000, currency:'USD' } }] as AppEvent[];
    const html=render(WorkloadDetails,{props:{task,events,runID:'run'}}).body;
    expect(html).toContain('Diagnostic analysis');expect(html).toContain('langchain-create-agent/v1');expect(html).toContain('Usage awaiting confirmation');expect(html).toContain('1,200');expect(html).toContain('Provider unavailable');expect(html).not.toContain('pull request');
  });
  it('does not reuse usage from an earlier run', () => {
    const events=[{ id:1,created_at:1,type:'model.usage',run_id:'old',message:'Old result',payload:{reported_tokens:99999} }] as AppEvent[];
    const html=render(WorkloadDetails,{props:{task,events,runID:'new'}}).body;
    expect(html).not.toContain('99,999');expect(html).toContain('No usage reported yet');
  });
  it('downloads analysis evidence from the displayed run instead of a previous result', () => {
    const proof: EvidenceRecord = {id:'proof',task_id:'task',run_id:'run',plan_revision_id:'plan',type:'test',producer:'worker',verifier:'marina:diagnostic-validator/v1',subject_digest:'report',source_revision:'input',command:'marina diagnostic-validator/v1',outcome:'passed',provenance_uri:'',provenance_digest:'proof',observed_at:1,artifact_uri:'',artifact_digest:'current-proof',admission_status:'current',created_at:1};
    const detail: TaskDetail = {
      task:{...task,status:'done'}, runs:[{id:'run',task_id:'task',agent_id:'agent',version:1,fence_token:1,task_version:1,acceptance_criteria_digest:'criteria',status:'completed',plan:'',result_summary:'Validated analysis https://marina.internal/artifacts/current-proof',started_at:1,updated_at:2}],
      plan_revisions:[],events:[],decisions:[],artifacts:[],evidence:[{...proof,id:'old',run_id:'old',artifact_digest:'old-proof'},proof],commands:[],operational_states:[],trail:[],display_state:'completed',
      provider_status:{project_id:'project',connectivity:'connected',sync_state:'current',sync_lag_millis:0,lease_state:'none',degraded_dependencies:[],stale_projection_count:0,rejected_projection_count:0}
    };
    const html=render(TaskDrawer,{props:{detail,onclose:()=>{},onrefresh:async()=>{},onmove:async()=>{},onsave:async()=>{},onanswer:async()=>{},onretention:async()=>{}}}).body;
    expect(html).toContain('/artifacts/current-proof');
    expect(html).not.toContain('/artifacts/old-proof');
    expect(html).toContain('Download validated analysis');
    expect(html).not.toContain('Review pull request');
  });
});
