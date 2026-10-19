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
  updateMailFields();
  for (const select of document.querySelectorAll('.setup-select')) {
    const trigger = select.querySelector('[role="combobox"]');
    const list = select.querySelector('[role="listbox"]');
    const input = select.querySelector('input');
    const options = [...list.querySelectorAll('[role="option"]')];
    const close = (focus = false) => {
      list.hidden = true;
      trigger.setAttribute('aria-expanded', 'false');
      if (focus) trigger.focus();
    };
    const open = (last = false) => {
      list.hidden = false;
      trigger.setAttribute('aria-expanded', 'true');
      (last ? options.at(-1) : options.find(o => o.getAttribute('aria-selected') === 'true') || options[0])?.focus();
    };
    trigger.addEventListener('click', () => list.hidden ? open() : close());
    trigger.addEventListener('keydown', e => {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); open(e.key === 'ArrowUp'); }
    });
    options.forEach((option, index) => {
      option.addEventListener('click', () => {
        input.value = option.dataset.value;
        trigger.querySelector('span').textContent = option.textContent;
        options.forEach(o => o.setAttribute('aria-selected', String(o === option)));
        close(true);
        updateMailFields();
      });
      option.addEventListener('keydown', e => {
        const keys = {ArrowDown: (index + 1) % options.length, ArrowUp: (index - 1 + options.length) % options.length, Home: 0, End: options.length - 1};
        if (e.key in keys) {e.preventDefault(); options[keys[e.key]].focus();}
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
