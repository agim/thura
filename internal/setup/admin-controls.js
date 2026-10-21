// Setup controls run within the official, administrator-gated page.
(() => {
  document.querySelector('.setup-steps [aria-current="step"]')?.scrollIntoView({block:"nearest",inline:"center"});
  const updateMailFields = () => {
    const value = name => document.getElementById(name)?.value;
    const localOut = value('THURA_MAIL_OUTBOUND') === 'postfix';
    const localIn = value('THURA_MAIL_INBOUND') === 'postfix';
    const provider = value('MAIL_PROVIDER');
    const show = (names, visible) => names.forEach(name => {
      const field = document.querySelector(`[data-setup-field="${name}"]`);
      if (field) field.hidden = !visible;
    });
    show(['THURA_POSTFIX_HOSTNAME'], localOut || localIn);
    show(['MAIL_PROVIDER'], !localOut);
    show(['MAIL_API_KEY', 'MAIL_REGION'], !localOut && provider !== 'smtp');
    show(['MAIL_DOMAIN'], !localOut && provider === 'mailgun');
    show(['MAIL_SMTP_HOST', 'MAIL_SMTP_PORT', 'MAIL_SMTP_SECURITY', 'MAIL_SMTP_USERNAME', 'MAIL_SMTP_PASSWORD'], !localOut && provider === 'smtp');
    show(['MAIL_INBOUND_SECRET'], !localIn);
  };
  const review = document.querySelector('[data-setup-review]');
  if (review && review.dataset.configured === 'true') {
    const status = review.querySelector('[data-review-status]');
    const sync = async () => {
      const response = await fetch(location.pathname + '?step=review', {credentials:'same-origin', cache:'no-store'});
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
        if (next && next.dataset.checkState !== row.dataset.checkState) {
          row.dataset.checkState = next.dataset.checkState;
          row.innerHTML = next.innerHTML;
        }
      }
      review.querySelector('[data-publish-button]').disabled = fresh.querySelector('[data-publish-button]').disabled;
      return [...review.querySelectorAll('[data-check-row]')];
    };
    const run = async form => {
      const button = form.querySelector('button');
      if (button) button.disabled = true;
      const row = form.closest('[data-check-row]');
      row.querySelector('[data-check-status]').textContent = 'Checking…';
      // A failed fetch is uncertain: never replay it or automatically create a new ID.
      const response = await fetch(form.action, {method:'POST', body:new URLSearchParams(new FormData(form)), credentials:'same-origin'});
      if (!response.ok) throw new Error('Check request failed. Reload to inspect its saved outcome before retrying.');
      const failure = new URL(response.url).searchParams.get('error');
      if (failure) throw new Error(failure);
      const page = new DOMParser().parseFromString(await response.text(), 'text/html');
      if (!page.querySelector('[data-setup-review]')) throw new Error('Check could not be confirmed. Reload to inspect its saved outcome.');
      await sync();
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
      finally { busy = false; }
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
      // Read-only follow-up for attempts already running in another tab.
      // Never resubmit an intent after a disconnect or reload.
      for (let reads = 0; reads < 12 && review.querySelector('[data-check-state="started"]'); reads++) {
        await new Promise(resolve => setTimeout(resolve, 5000));
        if (!document.contains(review)) break;
        try {
          const rows = await sync();
          if (rows.every(row => row.dataset.checkState === 'succeeded')) status.textContent = 'All connections passed. Ready to publish.';
        } catch (error) { status.textContent = error.message; break; }
      }
    })();
  }
  updateMailFields();
  const updateAIFields = () => {
    const provider = document.getElementById('LLM_PROVIDER')?.value;
    const field = document.querySelector('[data-setup-field="LLM_API_KEY"]');
    if (field) field.hidden = provider === 'ollama';
  };
  updateAIFields();
  for (const select of document.querySelectorAll('.setup-select')) {
    const trigger = select.querySelector('[role="combobox"]');
    const list = select.querySelector('[role="listbox"]');
    const input = select.querySelector('input[type=hidden]');
    const search = select.querySelector('[data-setup-search]');
    const options = [...list.querySelectorAll('[role="option"]')];
    const close = (focus = false) => {
      list.hidden = true;
      if (search) search.hidden = true;
      trigger.setAttribute('aria-expanded', 'false');
      if (focus) trigger.focus();
    };
    const open = (last = false) => {
      list.hidden = false;
      if (search) { search.hidden = false; search.value = ''; options.forEach(o => o.hidden = false); }
      trigger.setAttribute('aria-expanded', 'true');
      (last ? options.at(-1) : options.find(o => o.getAttribute('aria-selected') === 'true') || options[0])?.focus();
    };
    if (search) search.addEventListener('keydown', e => {
      if (e.key === 'Escape') { e.preventDefault(); close(true); }
      if (e.key === 'ArrowDown') { e.preventDefault(); options.find(o => !o.hidden)?.focus(); }
    });
    if (search) search.addEventListener('input', () => options.forEach(o => o.hidden = !o.textContent.toLowerCase().replaceAll('_',' ').includes(search.value.toLowerCase().replaceAll('_',' ')))) ;
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
        updateMailFields();
        updateAIFields();
      });
      option.addEventListener('keydown', e => {
        const visible = options.filter(o => !o.hidden);
        const at = visible.indexOf(option);
        const keys = {ArrowDown: (at + 1) % visible.length, ArrowUp: (at - 1 + visible.length) % visible.length, Home: 0, End: visible.length - 1};
        if (e.key in keys) {e.preventDefault(); visible[keys[e.key]]?.focus();}
        if (e.key === 'Escape') {e.preventDefault(); close(true);}
        if (e.key === 'Tab') close();
        if (e.key.length === 1 && e.key !== ' ') options.find(o => o.textContent.toLowerCase().startsWith(e.key.toLowerCase()))?.focus();
      });
    });
    document.addEventListener('pointerdown', e => {if (!select.contains(e.target)) close();});
    select.addEventListener('focusout', e => {if (!select.contains(e.relatedTarget)) close();});
  }
  for (const button of document.querySelectorAll('[data-setup-open]')) {
    const dialog = document.getElementById(button.dataset.setupOpen);
    button.addEventListener('click', () => dialog.showModal());
    dialog.querySelector('[data-setup-close]').addEventListener('click', () => dialog.close());
    dialog.addEventListener('click', e => {if (e.target === dialog && (e.clientX < dialog.getBoundingClientRect().left || e.clientX > dialog.getBoundingClientRect().right || e.clientY < dialog.getBoundingClientRect().top || e.clientY > dialog.getBoundingClientRect().bottom)) dialog.close();});
  }
})();
