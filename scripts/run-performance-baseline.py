#!/usr/bin/env python3
import argparse, json, subprocess, datetime, statistics, csv
from pathlib import Path


def parse_result(output: str):
    res=[]
    for line in output.splitlines():
        if 'forward benchmark result' in line and '{' in line:
            j=line[line.find('{'):]
            try:
                res.append(json.loads(j))
            except Exception:
                pass
    return res


def run_once(proto, model, pipeline, payload, workers, pps, duration, warmup, base_port):
    cmd=[
        './cmd/bench/main',
    ]
    # use go run to avoid stale binary
    cmd=['go','run','./cmd/bench',
         '-mode',proto,
         '-duration',duration,
         '-warmup',warmup,
         '-payload-size',str(payload),
         '-workers',str(workers),
         '-task-execution-model',model,
         '-pipeline-profile',pipeline,
         '-pps-per-worker',str(pps),
         '-base-port',str(base_port),
         '-log-level','info',
         '-traffic-stats-interval','60s']
    p=subprocess.run(cmd,capture_output=True,text=True)
    out=(p.stdout or '')+(p.stderr or '')
    results=parse_result(out)
    if p.returncode!=0 or not results:
        return {'ok':False,'cmd':' '.join(cmd),'exit_code':p.returncode,'error':'result_not_found'}
    r=results[-1]
    return {'ok':True,'cmd':' '.join(cmd),'exit_code':0,'result':r}


def scenario_name(s):
    return f"{s['proto']}_{s['model']}_{s['pipeline']}_p{s['payload']}_w{s['workers']}"


def choose_candidate(s, pps_candidates, pre_dur='2s', pre_warm='500ms', base_port_seed=40000):
    # fast pre-search: highest 0-loss pps from candidates
    best=None
    details=[]
    for i,pps in enumerate(pps_candidates):
        rr=run_once(s['proto'],s['model'],s['pipeline'],s['payload'],s['workers'],pps,pre_dur,pre_warm,base_port_seed+i)
        details.append({'pps_per_worker':pps,**rr})
        if rr.get('ok') and rr['result'].get('loss_rate',1)==0:
            best=pps
    return best,details


def formal_verify(s, pps, repeats, duration='30s', warmup='5s', base_port_seed=20000):
    runs=[]
    for i in range(repeats):
        rr=run_once(s['proto'],s['model'],s['pipeline'],s['payload'],s['workers'],pps,duration,warmup,base_port_seed+i*10)
        runs.append({'run':i+1,'pps_per_worker':pps,**rr})
    return runs


def summarize_runs(runs):
    oks=[r for r in runs if r.get('ok')]
    if len(oks)!=len(runs):
        return {'status':'failed','reason':'some_runs_failed'}
    losses=[r['result']['loss_rate'] for r in oks]
    if any(x>0 for x in losses):
        return {'status':'loss','avg_loss':sum(losses)/len(losses)}
    pps=[r['result']['pps'] for r in oks]
    mbps=[r['result']['mbps'] for r in oks]
    return {
        'status':'zero_loss',
        'avg_pps':sum(pps)/len(pps),
        'avg_mbps':sum(mbps)/len(mbps),
        'runs_pps':pps,
        'runs_mbps':mbps,
    }


def main():
    ap=argparse.ArgumentParser()
    ap.add_argument('--out',default='test-results/performance/2026-03-14-zero-loss-baseline')
    ap.add_argument('--repeats',type=int,default=3)
    args=ap.parse_args()

    out=Path(args.out)
    out.mkdir(parents=True,exist_ok=True)

    # key matrix: proto x model x pipeline, payload=512, workers=2
    scenarios=[]
    for proto in ['udp','tcp']:
        for model in ['fastpath','pool','channel']:
            scenarios.append({'type':'key','proto':proto,'model':model,'pipeline':'empty','payload':512,'workers':2})

    # pipeline complexity matrix on representative model/proto
    for pipeline in ['basic','complex']:
        scenarios.append({'type':'pipeline','proto':'udp','model':'pool','pipeline':pipeline,'payload':512,'workers':2})
        scenarios.append({'type':'pipeline','proto':'tcp','model':'pool','pipeline':pipeline,'payload':512,'workers':2})

    # payload matrix: proto x model(pool) x payload
    for proto in ['udp','tcp']:
        for payload in [256,4096]:
            scenarios.append({'type':'payload','proto':proto,'model':'pool','pipeline':'empty','payload':payload,'workers':2})

    # worker matrix: proto x model(pool) x workers
    for proto in ['udp','tcp']:
        for workers in [1,4]:
            scenarios.append({'type':'workers','proto':proto,'model':'pool','pipeline':'empty','payload':512,'workers':workers})

    pps_candidates={
        'udp':[1000,2000,4000,8000,12000],
        'tcp':[2000,4000,8000,12000,16000],
    }

    all_records=[]
    finals=[]
    cmd_records=[]

    for idx,s in enumerate(scenarios,1):
        name=scenario_name(s)
        print(f"[scenario {idx}/{len(scenarios)}] {name}", flush=True)
        best,pre=choose_candidate(s,pps_candidates[s['proto']],base_port_seed=41000+idx*100)
        # if no zero-loss candidate, keep smallest for formal evidence
        candidate=best if best is not None else pps_candidates[s['proto']][0]
        chosen=None
        chosen_summary=None
        tried=[]

        # verify from candidate downward until zero-loss
        ordered=[x for x in pps_candidates[s['proto']] if x<=candidate]
        ordered.sort(reverse=True)
        if not ordered:
            ordered=[candidate]
        candidates=ordered[:2] if len(ordered)>1 else ordered

        repeats = args.repeats if s['type']=='key' else 1
        for c in candidates:
            runs=formal_verify(s,c,repeats,base_port_seed=20000+idx*20)
            summ=summarize_runs(runs)
            tried.append({'pps_per_worker':c,'summary':summ,'runs':runs})
            if summ.get('status')=='zero_loss':
                chosen=c
                chosen_summary=summ
                break

        rec={
            'scenario':name,
            **s,
            'presearch':pre,
            'formal_trials':tried,
            'zero_loss_pps_per_worker':chosen,
            'zero_loss_total_pps_avg': (chosen_summary['avg_pps'] if chosen_summary else None),
            'zero_loss_total_mbps_avg': (chosen_summary['avg_mbps'] if chosen_summary else None),
            'zero_loss_run_pps': (chosen_summary['runs_pps'] if chosen_summary else []),
            'zero_loss_run_mbps': (chosen_summary['runs_mbps'] if chosen_summary else []),
            'zero_loss_judgement': 'all formal runs loss_rate == 0' if chosen_summary else 'not_reached',
        }
        all_records.append(rec)
        finals.append({
            'scenario':name,
            'matrix_type':s['type'],
            'proto':s['proto'],
            'execution_model':s['model'],
            'pipeline_profile':s['pipeline'],
            'payload_size':s['payload'],
            'workers':s['workers'],
            'formal_duration':'30s',
            'formal_repeats':repeats,
            'zero_loss_pps_per_worker':chosen,
            'zero_loss_total_pps_avg':rec['zero_loss_total_pps_avg'],
            'zero_loss_total_mbps_avg':rec['zero_loss_total_mbps_avg'],
            'zero_loss_judgement':rec['zero_loss_judgement'],
            'note':'' if chosen_summary else '0-loss not reached in tested candidates',
        })

    now=datetime.datetime.now(datetime.timezone.utc).isoformat()
    env={
      'timestamp_utc':now,
      'git_commit':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),
      'git_branch':subprocess.check_output(['git','rev-parse','--abbrev-ref','HEAD'],text=True).strip(),
      'go_version':subprocess.check_output(['go','version'],text=True).strip(),
      'os':subprocess.check_output(['uname','-srm'],text=True).strip(),
      'cpu_count':subprocess.check_output(['nproc'],text=True).strip(),
      'formal_duration':'30s',
      'formal_repeats_key':args.repeats,
      'formal_repeats_non_key':1,
      'presearch_duration':'5s',
      'presearch_warmup':'1s',
    }

    (out/'environment.json').write_text(json.dumps(env,indent=2))
    (out/'results-detail.json').write_text(json.dumps(all_records,indent=2))
    (out/'results-summary.json').write_text(json.dumps(finals,indent=2))

    with open(out/'results-summary.csv','w',newline='') as f:
        fields=list(finals[0].keys())
        w=csv.DictWriter(f,fieldnames=fields); w.writeheader(); w.writerows(finals)

    # md table
    lines=['|scenario|type|proto|model|pipeline|payload|workers|formal_runs|max_zero_loss_pps_avg|max_zero_loss_mbps_avg|pps_per_worker|judgement|',
           '|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---|']
    for r in finals:
        pps='N/A' if r['zero_loss_total_pps_avg'] is None else f"{r['zero_loss_total_pps_avg']:.2f}"
        mb='N/A' if r['zero_loss_total_mbps_avg'] is None else f"{r['zero_loss_total_mbps_avg']:.2f}"
        ppw='N/A' if r['zero_loss_pps_per_worker'] is None else str(r['zero_loss_pps_per_worker'])
        lines.append(f"|{r['scenario']}|{r['matrix_type']}|{r['proto']}|{r['execution_model']}|{r['pipeline_profile']}|{r['payload_size']}|{r['workers']}|{r['formal_repeats']}|{pps}|{mb}|{ppw}|{r['zero_loss_judgement']}|")
    (out/'results-summary.md').write_text('\n'.join(lines)+'\n')

if __name__=='__main__':
    main()
