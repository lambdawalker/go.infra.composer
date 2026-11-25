#!/usr/bin/env python3
"""Offline deployment command fixture. Never contacts a provider."""
import json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ['MOCK_LOG'], 'a') as log:
    log.write(json.dumps([name, *args]) + '\n')
def emit(value):
    print(json.dumps(value))
if name == 'go':
    if args[0] == 'version': print('go version go1.27.3 linux/amd64')
    elif args[0] == 'env': emit({})
    elif args[0] == 'list': emit({'Dir': os.getcwd(), 'GoFiles': ['source.go'], 'Module': {'GoMod': os.path.join(os.getcwd(),'go.mod')}})
    elif args[0] == 'run':
        if os.getenv('MOCK_PREPARE_FAIL'): sys.exit(14)
        out = pathlib.Path('../web/dist'); out.mkdir(parents=True, exist_ok=True)
        (out/'index.html').write_text('prepared')
    elif args[0] == 'build': pathlib.Path(args[args.index('-o')+1]).write_bytes(b'fake binary')
    else: sys.exit(8)
elif name == 'aws':
    if args == ['--version']: print('aws-cli/2.1.0')
    elif args[:2] == ['sts','get-caller-identity']: emit({'Account':'123456789012','Arn':'arn:aws:sts::123456789012:assumed-role/ci/session'})
    elif args[1] == 'get-bucket-versioning': emit('Enabled')
    elif args[1] == 'get-bucket-encryption': emit('AES256')
    elif args[1] == 'get-public-access-block': emit(dict.fromkeys(['BlockPublicAcls','BlockPublicPolicy','IgnorePublicAcls','RestrictPublicBuckets'], True))
    elif args[1] == 'get-bucket-location': emit('us-east-2')
    elif args[1] == 'get-bucket-policy': emit({'Policy':json.dumps({'Statement':[{'Effect':'Deny','Principal':'*','Action':'s3:*','Resource':['arn:aws:s3:::state','arn:aws:s3:::state/*'],'Condition':{'Bool':{'aws:SecureTransport':'false'}}}]})})
    elif args[1] in ['head-bucket','list-objects-v2']: emit({})
    else: sys.exit(9)
elif name == 'pulumi':
    if args == ['version']: print('v3.200.0')
    elif args[:2] == ['stack','ls']:
        if os.getenv('MOCK_LIST_FAIL'): sys.exit(10)
        emit([{'name':os.environ['MOCK_STACK']}])
    elif args[:2] == ['stack','export']: emit({'deployment':{'resources':[]}})
    elif args[:2] == ['stack','output']: emit({'bucket':'inputs','function':'benchmark','secret':'never-export'})
    elif args[:2] == ['config','--json']: emit({})
    elif args[0] == 'preview':
        if os.getenv('MOCK_PREVIEW_FAIL'): sys.exit(11)
        pathlib.Path(args[args.index('--save-plan')+1]).write_text(json.dumps({'resourcePlans':{'urn':{'steps':['delete' if os.getenv('MOCK_DESTRUCTIVE') else 'create']}}}))
        if os.getenv('MOCK_MUTATE_PATH'): pathlib.Path(os.environ['MOCK_MUTATE_PATH']).write_text('changed after preview')
    elif args[0] in ['login','stack','config','refresh','up']: pass
    else: sys.exit(12)
else: sys.exit(13)
