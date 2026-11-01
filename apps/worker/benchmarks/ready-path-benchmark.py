"""Local startup benchmark. Requires an isolated stack and web-env.json in STATE_DIR.
Usage: python3 ready-path-benchmark.py REPO LABEL STATE_DIR
Ports 3288 and 4298 must be reserved for this benchmark; see the companion report.
"""
import os,json,subprocess,pathlib,time,signal,sys,socket
repo=sys.argv[1];label=sys.argv[2]
root=pathlib.Path(sys.argv[3])
out=root/label;out.mkdir()
with socket.socket() as port_check:
 port_check.bind(('127.0.0.1',3288))
env={**os.environ,**json.loads((root/'web-env.json').read_text())}
for k in ['DEMO','NEXT_PUBLIC_DEMO','OPENNEKO_STACK_MODE']:env.pop(k,None)
base='http://localhost:3288/api/work/threads'
def req(url,data=None):
 args=['curl','-fsS','--max-time','120',url,'-H','Content-Type: application/json','-H','Origin: http://localhost:3288']
 if data is not None:args+=['--data',json.dumps(data)]
 return json.loads(subprocess.check_output(args,stderr=subprocess.PIPE))
results=[]
for scenario,pool,turns in [('warm-hot','1',3),('cold','0',1)]:
 for repetition in range(3):
  logpath=out/f'{scenario}-{repetition}.log'
  with open(logpath,'w') as log:
   process=subprocess.Popen(['pnpm','--filter','@neko/web','exec','next','dev','--port','3288'],cwd=repo,env={**env,'OPENNEKO_AGENT_WARM_POOL_SIZE':pool},stdout=log,stderr=log,start_new_session=True)
   try:
    for attempt in range(90):
     if process.poll() is not None:raise RuntimeError('benchmark server exited')
     try:req(base);break
     except (subprocess.CalledProcessError,json.JSONDecodeError):time.sleep(1)
    else:raise RuntimeError('benchmark server did not become ready')
    tid=req(base,{'title':f'Startup {label} {scenario} repetition {repetition}'})['thread']['id']
    for turn in range(turns):
     started=time.monotonic()
     run=req(base+'/'+tid+'/runs',{'message':'Read-only startup benchmark. What is 17 plus 25? Reply with the number only. Do not use tools, query data, write files, or change anything.'})
     ack=time.monotonic()-started;events=[]
     with subprocess.Popen(['curl','-fsSN','--max-time','240',base+'/'+tid+'/runs/'+run['runId']+'/events'],stdout=subprocess.PIPE) as stream:
      for raw in stream.stdout:
       line=raw.decode().strip()
       if line.startswith('data: '):events.append(json.loads(line[6:]))
     stream_code=stream.returncode
     deadline=time.monotonic()+240
     while True:
      bundle=req(base+'/'+tid)
      status=next(r['status'] for r in bundle['runs'] if r['id']==run['runId'])
      if status not in ('queued','running') or time.monotonic()>deadline:break
      time.sleep(0.25)
     record={'scenario':scenario,'repetition':repetition,'turn':turn,'threadId':tid,'runId':run['runId'],'ackSeconds':ack,'totalSeconds':time.monotonic()-started,'status':status,'events':events,'streamExitCode':stream_code}
     results.append(record);(out/'results.json').write_text(json.dumps(results))
     print(label,scenario,repetition,turn,status,run['runId'],round(record['totalSeconds'],3),flush=True)
     if status!='completed':raise RuntimeError('benchmark turn did not complete')
   finally:
    os.killpg(process.pid,signal.SIGTERM)
    try:process.wait(timeout=20)
    except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
    time.sleep(1)
(out/'metadata.json').write_text(json.dumps({'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),'imageId':subprocess.check_output(['docker','image','inspect','openneko-agent:eval','--format','{{.Id}}'],text=True).strip(),'runtime':'Next development server, real PostgreSQL/GraphJin/embedding/OpenShell/model; route compilation excluded from server startup phases'}))
