"""Reproducible research serving: frozen template and exact Go evidence encoding."""
import datetime
import json
from pathlib import Path


def evidence(text):
    # encoding/json escapes these characters even inside the JSON text field.
    value = json.dumps({'text': text}, ensure_ascii=False, separators=(',', ':'))
    for source, target in [('&', '\\u0026'), ('<', '\\u003c'), ('>', '\\u003e'), ('\u2028', '\\u2028'), ('\u2029', '\\u2029')]:
        value = value.replace(source, target)
    return value


def load_profile(path):
    if not path:
        return {}, []
    profile = json.loads(Path(path).read_text())
    if not isinstance(profile, dict) or type(profile.get('version')) is not int or profile['version'] not in [1, 2]:
        raise ValueError('unknown serving profile version')
    required = {'version', 'template_date', 'batch_size', 'repack'}
    if profile['version'] == 2:
        required |= {'cache_ram_mib', 'prompt_cache'}
    if set(profile) != required:
        raise ValueError('unknown serving profile contract')
    date = profile['template_date']
    if not isinstance(date, str) or datetime.datetime.strptime(date, '%d %b %Y').strftime('%d %b %Y') != date:
        raise ValueError('template date must be explicit and canonical')
    if type(profile['batch_size']) is not int or profile['batch_size'] != 32 or profile['repack'] is not False:
        raise ValueError('parity profile requires bounded batches and no weight repacking')
    flags = ['--no-repack', '--chat-template-kwargs', json.dumps({'date_string': date})]
    if profile['version'] == 2:
        if type(profile['cache_ram_mib']) is not int or profile['cache_ram_mib'] != 0 or profile['prompt_cache'] is not False:
            raise ValueError('stateless screening must disable cross-message RAM caching')
        flags += ['--cache-ram', '0', '--no-cache-prompt', '--no-cache-idle-slots']
    return profile, flags
