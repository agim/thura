"""Explicit free local service choices for the owned acceptance installation."""
import json
from pathlib import Path


def configuration(filename=None):
    values={
        'ai':{'THURA_AI_ENABLED':'true','LLM_PROVIDER':'ollama'},
        'integrations':{'SMIP_ENABLED':'true'},
        'mail':{'SMIP_MAIL_ENABLED':'true'},
    }
    if not filename:return values
    path=Path(filename)
    if not path.is_file() or path.is_symlink():raise RuntimeError('Core-service configuration must be a private regular file')
    if path.stat().st_mode & 0o077:raise RuntimeError('Core-service credentials file must be private (chmod 600)')
    if path.stat().st_size>131072:raise RuntimeError('Core-service configuration exceeds its size limit')
    overrides=json.loads(path.read_text())
    allowed={
        'ai':{'THURA_AI_ENABLED','LLM_PROVIDER','LLM_MODEL','LLM_BASE_URL','LLM_API_KEY','LLM_MAX_TOKENS'},
        'integrations':{'MATRIX_SERVER_URL','MATRIX_SERVER_NAME','MATRIX_AS_TOKEN','COLLABORA_SERVER_URL','COLLABORA_APP_URL','COLLABORA_TOKEN_SECRET','LIVEKIT_SERVER_URL','LIVEKIT_PUBLIC_URL','LIVEKIT_API_KEY','LIVEKIT_API_SECRET','SMIP_ENABLED','SMIP_CONFIG'},
        'mail':{'SMIP_MAIL_ENABLED','SMIP_MAIL_CONFIG','smip_mail_domain'},
    }
    if not isinstance(overrides,dict) or not set(overrides)<=set(allowed):raise RuntimeError('Core configuration supports only ai, integrations and mail objects')
    for step,fields in overrides.items():
        if not isinstance(fields,dict) or not set(fields)<=allowed[step] or not all(isinstance(value,str) for value in fields.values()):raise RuntimeError('Core configuration contains unsupported fields or values')
        values[step].update(fields)
    return values
