// Setup controls run within the official, administrator-gated page.
(() => {
  document.querySelector('.setup-steps [aria-current="step"]')?.scrollIntoView({block:"nearest",inline:"center"});
  // A field's current value, with checkboxes read as the strings the server stores.
  const current = name => {
    const element = document.getElementById(name);
    if (!element) return undefined;
    return element.type === 'checkbox' ? (element.checked ? 'true' : 'false') : element.value;
  };
  // Visibility is declared by the server as data-show-when: a list of
  // alternatives, each mapping a field name to its allowed values. A field
  // whose controlling field is itself hidden stays hidden.
  const rules = new Map();
  for (const element of document.querySelectorAll('[data-show-when]')) {
    try { rules.set(element, JSON.parse(element.dataset.showWhen)); } catch { rules.set(element, []); }
  }
  const holds = (alternatives, depth) => alternatives.some(condition => Object.entries(condition).every(([name, allowed]) => allowed.includes(current(name)) && fieldVisible(name, depth + 1)));
  const fieldVisible = (name, depth = 0) => {
    const field = document.querySelector(`[data-setup-field="${name}"]`);
    const alternatives = field && rules.get(field);
    return !alternatives || depth > 8 || holds(alternatives, depth);
  };
  const applyVisibility = () => { for (const [element, alternatives] of rules) element.hidden = !holds(alternatives, 0); };
  // Placeholders show what a blank field receives on save; they follow edits live.
  const applyDerived = () => {
    for (const input of document.querySelectorAll('[data-derive-from]')) {
      const source = current(input.dataset.deriveFrom) || '';
      let hint = '';
      if (input.dataset.deriveRule === 'email-domain') {
        const at = source.lastIndexOf('@');
        hint = at > 0 && at < source.length - 1 ? source.slice(at + 1).toLowerCase() : '';
      } else if (input.dataset.deriveRule === 'map') {
        try { hint = JSON.parse(input.dataset.deriveMap || '{}')[source] || ''; } catch { hint = ''; }
      }
      input.placeholder = hint;
    }
  };
  const updateStorageFields = () => {
    const backend = document.getElementById('STORAGE_PROVIDER');
    if (!backend) return;
    const cloud = backend.value === 's3';
    const profile = document.getElementById('THURA_STORAGE_PROFILE').value;
    const location = document.getElementById('THURA_STORAGE_LOCATION');
    const set = (name, value) => {
      const input = document.getElementById(name);
      if (!input) return;
      input.value = value;
      const select = input.closest('.setup-select');
      if (select) {
        const options = [...select.querySelectorAll('[role=option]')];
        options.forEach(option => option.setAttribute('aria-selected', String(option.dataset.value === value)));
        select.querySelector('[role=combobox] span').textContent = options.find(option => option.dataset.value === value)?.textContent || value || 'Choose an option';
      }
    };
    const options = [...location.closest('.setup-select').querySelectorAll('[role=option]')];
    options.forEach(option => option.hidden = option.dataset.storageProfile !== profile);
    if (cloud && (profile === 'aws' || profile === 'digitalocean')) {
      if (!options.some(option => !option.hidden && option.dataset.value === location.value)) set('THURA_STORAGE_LOCATION',profile === 'aws' ? 'us-east-1' : 'nyc3');
      set('STORAGE_ENDPOINT',profile === 'aws' ? `https://s3.${location.value}.amazonaws.com` : `https://${location.value}.digitaloceanspaces.com`);
      set('STORAGE_REGION',profile === 'aws' ? location.value : 'us-east-1');
    }
    if (cloud && profile === 'google') { set('STORAGE_ENDPOINT','https://storage.googleapis.com'); set('STORAGE_REGION','auto'); }
    // Known providers derive the endpoint and signing region from the bucket region.
    document.getElementById('STORAGE_ENDPOINT').readOnly = profile !== 'custom';
    document.getElementById('STORAGE_REGION').readOnly = profile !== 'custom';
    const hints = {
      google:'Use a Cloud Storage HMAC access ID and HMAC secret for a service account. Service-account JSON keys and Google API keys do not work with this XML/S3 connection.',
      digitalocean:'Select your Spaces bucket region and use a Spaces access key pair. The service endpoint and signing region are configured automatically; do not use a CDN URL.',
      aws:'Select the bucket’s AWS region and use an IAM access key with access to this bucket and installation prefix. The regional S3 service endpoint is configured automatically.',
      custom:'Enter your S3-compatible service origin and signing region. Use a dedicated key limited to this bucket and installation prefix.'
    };
    const help = document.querySelector('[data-storage-profile-help]');
    help.hidden = !cloud;
    help.textContent = hints[profile] + ' Keep the bucket private. Saved keys are not reused across different providers.';
    document.getElementById('label-STORAGE_ACCESS_KEY').textContent = profile === 'google' ? 'HMAC access ID' : profile === 'digitalocean' ? 'Spaces access key' : 'S3 access key';
    document.getElementById('label-STORAGE_SECRET_KEY').textContent = profile === 'google' ? 'HMAC secret' : profile === 'digitalocean' ? 'Spaces secret key' : 'S3 secret key';
  };
  const suggestRefreshers = [];
  const refresh = () => {
    applyVisibility();
    applyDerived();
    updateStorageFields();
    suggestRefreshers.forEach(fn => fn());
  };
  const review = document.querySelector('[data-setup-review]');
  if (review && review.dataset.configured === 'true') {
    const status = review.querySelector('[data-review-status]');
    const sync = async () => {
      const response = await fetch(location.pathname + '?step=review', {credentials:'same-origin', cache:'no-store', signal:AbortSignal.timeout(15000)});
      if (!response.ok) throw new Error('Could not read check results. Reload to inspect the saved outcome.');
      const page = new DOMParser().parseFromString(await response.text(), 'text/html');
      const fresh = page.querySelector('[data-setup-review]');
      if (!fresh) throw new Error('Your session expired. Reload to sign in and inspect the saved outcome.');
      if (fresh.querySelector('[name=revision]').value !== review.querySelector('[name=revision]').value) {
        review.querySelector('[data-publish-button]').disabled = true;
        throw new Error('Setup changed in another tab. Reload to check the updated configuration.');
      }
      for (const row of review.querySelectorAll('[data-check-row]')) {
        const next = fresh.querySelector(`[data-check-row="${row.dataset.checkRow}"]`);
        if (!next) continue;
        const changed = row.dataset.checkState !== next.dataset.checkState;
        row.dataset.checkState = next.dataset.checkState;
        row.dataset.checkAttempt = next.dataset.checkAttempt;
        row.querySelector('[data-check-status]').textContent = next.querySelector('[data-check-status]').textContent;
        const form = row.querySelector('[data-check-form]');
        const nextForm = next.querySelector('[data-check-form]');
        form.setAttribute('action', nextForm.getAttribute('action'));
        form.toggleAttribute('data-auto-check', nextForm.hasAttribute('data-auto-check'));
        for (const input of nextForm.querySelectorAll('input[name]')) form.elements.namedItem(input.name).value = input.value;
        let button = form.querySelector('button');
        const nextButton = nextForm.querySelector('button');
        if (!nextButton) button?.remove();
        else {
          if (!button) { button = nextButton.cloneNode(true); form.append(button); }
          button.textContent = nextButton.textContent;
          button.disabled = false;
        }
        const details = row.querySelector('details');
        const nextDetails = next.querySelector('details');
        if (!nextDetails) details?.remove();
        else if (!details) row.append(nextDetails.cloneNode(true));
        else {
          if (changed) details.open = nextDetails.open;
          const text = details.querySelector('p');
          text.textContent = nextDetails.querySelector('p').textContent;
        }
      }
      review.querySelector('[data-publish-button]').disabled = fresh.querySelector('[data-publish-button]').disabled;
      return [...review.querySelectorAll('[data-check-row]')];
    };
    let watching = false;
    async function watchStarted() {
      if (watching) return;
      watching = true;
      try {
        for (let reads = 0; reads < 12 && review.querySelector('[data-check-state="started"]'); reads++) {
          await new Promise(resolve => setTimeout(resolve, 5000));
          if (!document.contains(review)) break;
          try {
            const rows = await sync();
            if (rows.every(row => row.dataset.checkState === 'succeeded')) status.textContent = 'All connections passed. Ready to publish.';
          } catch (error) { status.textContent = error.message; break; }
        }
      } finally { watching = false; }
    }
    const run = async form => {
      const button = form.querySelector('button');
      if (button) button.disabled = true;
      const row = form.closest('[data-check-row]');
      row.querySelector('[data-check-status]').textContent = 'Checking…';
      try {
      // A failed fetch is uncertain: never replay it or automatically create a new ID.
      const response = await fetch(form.action, {method:'POST', body:new URLSearchParams(new FormData(form)), credentials:'same-origin', signal:AbortSignal.timeout(75000)});
      if (!response.ok) throw new Error('Check request failed. Reload to inspect its saved outcome before retrying.');
      const failure = new URL(response.url).searchParams.get('error');
      if (failure) throw new Error(failure);
      const page = new DOMParser().parseFromString(await response.text(), 'text/html');
      if (!page.querySelector('[data-setup-review]')) throw new Error('Check could not be confirmed. Reload to inspect its saved outcome.');
      await sync();
      } catch (error) {
        // A read can recover a lost response without replaying the submission.
        try { await sync(); } catch {
          row.querySelector('[data-check-status]').textContent = 'Outcome not confirmed';
        }
        throw error;
      } finally {
        row.querySelector('button')?.removeAttribute('disabled');
      }
    };
    let busy = true;
    review.addEventListener('submit', async event => {
      const form = event.target.closest('[data-check-form]');
      if (!form) return;
      event.preventDefault();
      if (busy) return;
      busy = true;
      try { await run(form); status.textContent = 'Checks updated.'; }
      catch (error) { status.textContent = error.message; }
      finally { busy = false; void watchStarted(); }
    });
    (async () => {
      try {
        // Sequential checks avoid boot-time memory spikes on the 4 GiB profile.
        const kinds = [...review.querySelectorAll('[data-auto-check]')].map(form => form.closest('[data-check-row]').dataset.checkRow);
        for (const kind of kinds) {
          const form = review.querySelector(`[data-check-row="${kind}"] [data-auto-check]`);
          if (form) await run(form);
        }
        const rows = await sync();
        status.textContent = rows.every(row => row.dataset.checkState === 'succeeded') ? 'All connections passed. Ready to publish.' : 'Checks complete. Review the services needing attention.';
      } catch (error) { status.textContent = error.message; }
      finally { busy = false; }
      // Follow interrupted and other-tab attempts by reading, never resending.
      await watchStarted();
    })();
  }
  const optionKeys = (options, option, close, e) => {
    const visible = options().filter(o => !o.hidden);
    const at = visible.indexOf(option);
    const keys = {ArrowDown: (at + 1) % visible.length, ArrowUp: (at - 1 + visible.length) % visible.length, Home: 0, End: visible.length - 1};
    if (e.key in keys) {e.preventDefault(); visible[keys[e.key]]?.focus();}
    if (e.key === 'Escape') {e.preventDefault(); close(true);}
    if (e.key === 'Tab') close();
  };
  for (const select of document.querySelectorAll('.setup-select:not(.setup-suggest)')) {
    const trigger = select.querySelector('[role="combobox"]');
    const list = select.querySelector('[role="listbox"]');
    const input = select.querySelector('input[type=hidden]');
    const search = select.querySelector('[data-setup-search]');
    const options = [...list.querySelectorAll('[role="option"]')];
    const belongs = option => input.id !== 'THURA_STORAGE_LOCATION' || option.dataset.storageProfile === document.getElementById('THURA_STORAGE_PROFILE').value;
    const close = (focus = false) => {
      list.hidden = true;
      if (search) search.hidden = true;
      trigger.setAttribute('aria-expanded', 'false');
      if (focus) trigger.focus();
    };
    const open = (last = false) => {
      list.hidden = false;
      if (search) { search.hidden = false; search.value = ''; options.forEach(o => o.hidden = !belongs(o)); }
      trigger.setAttribute('aria-expanded', 'true');
      (last ? options.at(-1) : options.find(o => !o.hidden && o.getAttribute('aria-selected') === 'true') || options.find(o => !o.hidden))?.focus();
    };
    if (search) search.addEventListener('keydown', e => {
      if (e.key === 'Escape') { e.preventDefault(); close(true); }
      if (e.key === 'ArrowDown') { e.preventDefault(); options.find(o => !o.hidden)?.focus(); }
    });
    if (search) search.addEventListener('input', () => options.forEach(o => o.hidden = !belongs(o) || !o.textContent.toLowerCase().replaceAll('_',' ').includes(search.value.toLowerCase().replaceAll('_',' ')))) ;
    trigger.addEventListener('click', () => { if (list.hidden) { open(); search?.focus(); } else close(); });
    trigger.addEventListener('keydown', e => {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); open(e.key === 'ArrowUp'); }
    });
    options.forEach(option => {
      option.addEventListener('click', () => {
        input.value = option.dataset.value;
        trigger.querySelector('span').textContent = option.textContent;
        options.forEach(o => o.setAttribute('aria-selected', String(o === option)));
        close(true);
        refresh();
      });
      option.addEventListener('keydown', e => {
        optionKeys(() => options, option, close, e);
        if (e.key.length === 1 && e.key !== ' ') options.find(o => o.textContent.toLowerCase().startsWith(e.key.toLowerCase()))?.focus();
      });
    });
    document.addEventListener('pointerdown', e => {if (!select.contains(e.target)) close();});
    select.addEventListener('focusout', e => {if (!select.contains(e.relatedTarget)) close();});
  }
  // A suggest control keeps free text and offers options; LLM_MODEL filters
  // them by the selected provider and adds the local service's installed
  // models through the gated models.json endpoint, never by contacting it here.
  for (const box of document.querySelectorAll('.setup-suggest')) {
    const input = box.querySelector('[data-setup-suggest]');
    const list = box.querySelector('[role="listbox"]');
    const toggle = box.querySelector('.setup-suggest-toggle');
    const by = input.dataset.suggestBy;
    const live = input.dataset.suggestLive;
    let loaded = false;
    const options = () => [...list.querySelectorAll('[role="option"]')];
    const belongs = option => !by || !option.dataset.group || option.dataset.group === current(by);
    const close = (focus = false) => { list.hidden = true; input.setAttribute('aria-expanded', 'false'); if (focus) input.focus(); };
    // Typing narrows the list; the toggle and arrow keys show every suggestion.
    let typed = '';
    const filter = () => {
      const query = typed.trim().toLowerCase();
      let any = false;
      for (const option of options()) {
        option.hidden = !belongs(option) || (query !== '' && !option.dataset.value.toLowerCase().includes(query) && !option.textContent.toLowerCase().includes(query));
        option.setAttribute('aria-selected', String(option.dataset.value === input.value));
        any = any || !option.hidden;
      }
      return any;
    };
    const open = query => { typed = query; if (!filter()) { close(); return false; } list.hidden = false; input.setAttribute('aria-expanded', 'true'); return true; };
    const wire = option => {
      option.addEventListener('click', () => {
        input.value = option.dataset.value;
        close(true);
        applyDerived();
      });
      option.addEventListener('keydown', e => optionKeys(options, option, close, e));
    };
    options().forEach(wire);
    const loadLive = async () => {
      if (!live || loaded || current(by) !== live) return;
      loaded = true;
      try {
        const response = await fetch('setup/models.json?provider=' + encodeURIComponent(live), {credentials:'same-origin', cache:'no-store', signal:AbortSignal.timeout(5000)});
        if (!response.ok) return;
        const data = await response.json();
        for (const name of Array.isArray(data.models) ? data.models : []) {
          if (typeof name !== 'string' || options().some(option => option.dataset.value === name && option.dataset.group === live)) continue;
          const option = document.createElement('button');
          option.type = 'button';
          option.setAttribute('role', 'option');
          option.setAttribute('aria-selected', 'false');
          option.dataset.value = name;
          option.dataset.group = live;
          option.textContent = name + ' · installed';
          list.append(option);
          wire(option);
        }
        if (!list.hidden) filter();
      } catch { /* no suggestions */ }
    };
    input.addEventListener('input', () => { void loadLive(); open(input.value); });
    input.addEventListener('focus', () => { void loadLive(); });
    input.addEventListener('keydown', e => {
      if (e.key === 'ArrowDown') { e.preventDefault(); if (open('')) options().find(o => !o.hidden)?.focus(); }
      if (e.key === 'Escape') { e.preventDefault(); close(); }
    });
    toggle.addEventListener('click', () => { if (list.hidden) { void loadLive(); open(''); } else close(true); });
    document.addEventListener('pointerdown', e => {if (!box.contains(e.target)) close();});
    box.addEventListener('focusout', e => {if (!box.contains(e.relatedTarget)) close();});
    suggestRefreshers.push(() => { void loadLive(); if (!list.hidden && !filter()) close(); });
  }
  for (const checkbox of document.querySelectorAll('[data-setup-field] input[type=checkbox][id]')) checkbox.addEventListener('change', refresh);
  document.addEventListener('input', e => { if (e.target.matches('[data-setup-field] input')) applyDerived(); });
  refresh();
  for (const button of document.querySelectorAll('[data-setup-open]')) {
    const dialog = document.getElementById(button.dataset.setupOpen);
    button.addEventListener('click', () => dialog.showModal());
    dialog.querySelector('[data-setup-close]').addEventListener('click', () => dialog.close());
    dialog.addEventListener('click', e => {if (e.target === dialog && (e.clientX < dialog.getBoundingClientRect().left || e.clientX > dialog.getBoundingClientRect().right || e.clientY < dialog.getBoundingClientRect().top || e.clientY > dialog.getBoundingClientRect().bottom)) dialog.close();});
  }
})();
