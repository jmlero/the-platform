#!/usr/bin/env python3
"""Exercise durable conversations through isolated real Deck processes."""
import hashlib, http.cookiejar, json, os, pathlib, secrets, socket, subprocess, tempfile, time, urllib.error, urllib.request

with tempfile.TemporaryDirectory(prefix='chat02-') as directory:
    work = pathlib.Path(directory)
    binary = work / 'deck'
    subprocess.run(['go', 'build', '-o', str(binary), './cmd/deck'], cwd=pathlib.Path(__file__).resolve().parents[1], check=True)
    token = secrets.token_urlsafe(40)
    auth = work / 'operators.json'
    auth.write_text(json.dumps({'version':1,'operators':[{'id':'chat02-test','token_sha256':[hashlib.sha256(token.encode()).hexdigest()]}]}))
    auth.chmod(0o600)
    service_token = secrets.token_urlsafe(40)
    service_auth = work / 'service-auth.json'
    service_auth.write_text(json.dumps({'version':1,'identities':[{'id':'chat04-test','token_sha256':[hashlib.sha256(service_token.encode()).hexdigest()],'projects':['prj_default'],'capabilities':['agent.register','conversation.invocation.list','conversation.question.write','conversation.outcome.publish','conversation.task.delegate','workload.publish']}]}))
    service_auth.chmod(0o600)
    def port():
        with socket.socket() as sock:
            sock.bind(('127.0.0.1',0))
            return sock.getsockname()[1]
    def start(data,listen):
        log=(work / (data+'.log')).open('ab')
        env={**{key:value for key,value in os.environ.items() if not key.startswith(('DECK_', 'APPDECK_'))},'DECK_ADDR':f'127.0.0.1:{listen}','DECK_DATA_DIR':str(work / data),'DECK_DEMO_AGENT':'false','DECK_OPERATOR_AUTH_FILE':str(auth),'DECK_OPERATOR_COOKIE_SECURE':'false','DECK_SERVICE_AUTH_FILE':str(service_auth)}
        process=subprocess.Popen([str(binary)],env=env,stdout=log,stderr=log)
        log.close()
        for _ in range(100):
            try:
                with urllib.request.urlopen(f'http://127.0.0.1:{listen}/healthz',timeout=1) as response:
                    if response.status==200:return process
            except (OSError,urllib.error.URLError):pass
            if process.poll() is not None:raise AssertionError('Deck exited before readiness: ' + (work / (data+'.log')).read_text())
            time.sleep(.05)
        process.terminate();process.wait(timeout=10)
        raise AssertionError('Deck did not become healthy')
    def stop(process):
        process.terminate();process.wait(timeout=10)
    class Session:
        def __init__(self,listen):
            self.url=f'http://127.0.0.1:{listen}'
            self.cookies=http.cookiejar.CookieJar()
            self.client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
        def request(self,method,path,data=None):
            headers={'Content-Type':'application/json'}
            if method not in ('GET','HEAD'):
                for cookie in self.cookies:
                    if cookie.name=='deck_operator_csrf':headers['X-CSRF-Token']=cookie.value
            req=urllib.request.Request(self.url+'/api/v1'+path,method=method,headers=headers,data=None if data is None else json.dumps(data).encode())
            with self.client.open(req,timeout=10) as response:
                body=response.read()
                return None if not body else json.loads(body)
        def login(self):self.request('POST','/operator/session',{'token':token})
    def service_request(listen, method, path, data=None):
        req=urllib.request.Request(f'http://127.0.0.1:{listen}/api/v1'+path,method=method,headers={'Content-Type':'application/json','Authorization':'Bearer '+service_token},data=None if data is None else json.dumps(data).encode())
        with urllib.request.urlopen(req,timeout=10) as response:
            body=response.read()
            return None if not body else json.loads(body)
    listen=port();process=start('first',listen);clone=None
    try:
        a,b=Session(listen),Session(listen)
        a.login();b.login()
        channel=a.request('POST','/operator/channels',{'project_id':'prj_default','name':'chat02-acceptance'})
        path='/operator/channels/'+channel['id']
        root=a.request('POST',path+'/messages',{'body':'Investigate the login issue.'})
        thread=a.request('POST',path+'/threads',{'root_message_id':root['id']})
        replay=b.request('POST',path+'/threads',{'root_message_id':root['id']})
        assert thread['id']==replay['id']
        reply=b.request('POST',path+'/messages',{'body':'The GitHub provider is affected.','thread_id':thread['id']})
        service_request(listen,'POST','/agents',{'id':'agt_chat04','installation_id':'chat04-test','name':'Conversation proof','capabilities':['conversation.invoke.prj_default']})
        invocation=a.request('POST',path+'/invocations',{'message_id':root['id'],'agent_id':'agt_chat04','request_id':'chat04-proof'})
        feed=service_request(listen,'GET','/conversation/invocations?project_id=prj_default&after_id=0')['items']
        assert len(feed)==1 and feed[0]['message']['id']==root['id'] and feed[0]['message']['thread_id']==invocation['thread']['id']
        assert feed[0]['invoked_at']>=root['created_at']
        assert service_request(listen,'GET',f"/conversation/invocations?project_id=prj_default&after_id={feed[0]['event_id']}")['items']==[]
        question_input={'invocation_message_id':root['id'],'agent_id':'agt_chat04','question_id':'provider','text':'Which login provider?','choices':['GitHub','Google']}
        question=service_request(listen,'POST','/conversation/questions',question_input)
        assert question['status']=='pending' and question['thread_id']==thread['id']
        assert service_request(listen,'POST','/conversation/questions',question_input)==question
        task=a.request('POST','/operator/messages/'+reply['id']+'/promote',{'title':'Investigate login','acceptance_criteria':'Record a diagnosis.','status':'backlog','priority':'medium','budget':{'time_minutes':30,'max_attempts':2,'max_tokens':10000,'max_cost_minor':100,'currency':'EUR','max_concurrency':1,'authority':['source.read']}})
        origin='/operator/tasks/'+task['id']+'/thread'
        assert b.request('GET',origin)['id']==thread['id']
        base='/operator/threads/'+thread['id']
        history=a.request('GET',base+'/messages?limit=1')
        cursor=history['next_cursor']
        assert history['items'][0]['id']==root['id']
        stop(process);process=start('first',listen)
        # The same independent browser sessions and cursor remain valid.
        assert a.request('GET',origin)['id']==thread['id']
        resumed=b.request('GET',base+'/messages?limit=1&cursor='+cursor)
        assert [m['id'] for m in resumed['items']]==[reply['id']]
        assert service_request(listen,'GET','/conversation/invocations?project_id=prj_default&after_id=0')['items']==feed
        assert a.request('GET',base+'/questions')['items']==[question]
        answer_input={'answer':'GitHub','request_id':'answer-provider-once'}
        answered=b.request('POST','/operator/conversation/questions/'+question['id']+'/answer',answer_input)
        assert answered['status']=='answered' and answered['thread_id']==thread['id']
        assert a.request('POST','/operator/conversation/questions/'+question['id']+'/answer',answer_input)['answer_message_id']==answered['answer_message_id']
        assert service_request(listen,'POST','/conversation/questions',question_input)==answered
        assert [m['id'] for m in a.request('GET',base+'/messages')['items'] if m['id']==answered['answer_message_id']]==[answered['answer_message_id']]
        a.request('PUT','/operator/tasks/'+task['id']+'/archive',{'version':task['version'],'archived':True})
        assert b.request('GET',origin)['task_ids']==[task['id']]
        delegated=a.request('POST',path+'/invocations',{'thread_id':thread['id'],'agent_id':'agt_chat04','body':'Draft a bounded follow-up.','request_id':'draft-followup'})
        outcome_input={'invocation_message_id':delegated['message']['id'],'agent_id':'agt_chat04','status':'completed','body':'I created a Backlog draft.','result_digest':'sha256:conversation-outcome','task':{'title':'Review the follow-up','status':'backlog','acceptance_criteria':'Record findings.'}}
        outcome=service_request(listen,'POST','/conversation/outcomes',outcome_input)
        assert outcome['task_id'] and service_request(listen,'POST','/conversation/outcomes',outcome_input)==outcome
        assert outcome['task_id'] in b.request('GET',base)['task_ids']
        assert [m['id'] for m in a.request('GET',base+'/messages')['items'] if m['id']==outcome['message_id']]==[outcome['message_id']]
        profile_digest='a'*64
        service_request(listen,'PUT','/projects/prj_default/workloads',{'expires_at':int(time.time()*1000)+240000,'profiles':[{'id':'repository/v1','digest':profile_digest,'harness':'codex/v1','authority':['source.read','source.write','pull_request.create'],'max_tokens':30000,'budget_minutes':40,'min_attempts':2}]})
        ready_invocation=a.request('POST',path+'/invocations',{'thread_id':thread['id'],'agent_id':'agt_chat04','body':'Fix the login regression and verify the result.','request_id':'ready-delegation'})
        ready_input={'invocation_message_id':ready_invocation['message']['id'],'agent_id':'agt_chat04','status':'completed','body':'The bounded task is ready.','result_digest':'sha256:ready-result','profile_id':'repository/v1','profile_digest':profile_digest,'task':{'title':'Fix login regression','status':'ready','acceptance_criteria':'Login regression checks pass.','budget':{'time_minutes':30,'max_attempts':2,'max_tokens':20000,'max_concurrency':1,'authority':['source.read','source.write','pull_request.create']}}}
        ready=service_request(listen,'POST','/conversation/delegations',ready_input)
        assert ready['task_id'] and service_request(listen,'POST','/conversation/delegations',ready_input)==ready
        assert b.request('GET','/operator/tasks/'+ready['task_id'])['task']['status']=='ready'
        assert b.request('GET','/operator/tasks/'+ready['task_id']+'/thread')['id']==thread['id']
        final_feed=service_request(listen,'GET','/conversation/invocations?project_id=prj_default&after_id=0')['items']
        bundle=a.request('GET','/operator/export?project_id=prj_default')
        cloneport=port();clone=start('restored',cloneport)
        restored=Session(cloneport);restored.login()
        restored.request('POST','/operator/import',bundle)
        assert restored.request('GET',origin)==a.request('GET',origin)
        assert restored.request('GET',base+'/messages')==a.request('GET',base+'/messages')
        assert restored.request('GET',base+'/questions')==a.request('GET',base+'/questions')
        assert service_request(cloneport,'POST','/conversation/outcomes',outcome_input)==outcome
        assert service_request(cloneport,'POST','/conversation/delegations',ready_input)==ready
        assert service_request(cloneport,'GET','/conversation/invocations?project_id=prj_default&after_id=0')['items']==final_feed
        assert restored.request('GET','/operator/export?project_id=prj_default')==bundle
        print('PASS: two authenticated sessions, stable thread and Backlog/Ready outcome replay, question and answer binding across restart, older-message invocation feed, promoted-task origin, archive, and export/import into a second real Deck process.')
    finally:
        if process.poll() is None:stop(process)
        if clone is not None and clone.poll() is None:stop(clone)
