import React, { useState } from "react";
import { h } from "../lib/ui.js";
import { useStoredState } from "../lib/storage.js";

const samples = [
  {
    id: 1,
    name: "Sarah Chen",
    email: "sarah@example.com",
    subject: "Ready for the next chapter?",
    excerpt: "The launch assets are approved. Here’s the final brief.",
    body: "Hi Agim,\n\nEverything is coming together. The new website assets are approved and the launch brief is ready for your review.\n\nI’ve attached the final version with our milestones, content checklist, and a few ideas for the announcement. Let’s walk through it at 2:30 today and decide what goes live first.\n\nThanks for keeping things moving.\nSarah",
    time: "1:12 PM",
    rank: 8,
    folder: "Inbox",
    label: "Work",
    unread: false,
    star: true,
    file: "Launch brief.pdf",
    size: "1.8 MB",
  },
  {
    id: 2,
    name: "Marcus Williams",
    email: "marcus@example.com",
    subject: "Brand assets / final delivery",
    excerpt: "Fresh colors. Clear typography. Everything is in the folder.",
    body: "Hi Agim,\n\nThe final brand assets are attached. The team has approved the new logos and photography.\n\nMarcus",
    time: "12:48 PM",
    rank: 7,
    folder: "Inbox",
    label: "Work",
    unread: true,
    star: false,
    file: "Brand assets.zip",
    size: "8.2 MB",
  },
  {
    id: 3,
    name: "Field Notes",
    email: "notes@example.com",
    subject: "Good work deserves good tools",
    excerpt: "A few things we’re reading, making, and thinking about.",
    body: "This week’s field notes:\n\nA collection of practical tools and ideas for independent builders.\n\nYou can move this message into Promotions or archive it when you’re done.",
    time: "11:06 AM",
    rank: 6,
    folder: "Inbox",
    label: "Promotions",
    unread: true,
    star: false,
  },
  {
    id: 4,
    name: "Liridon Shabani",
    email: "liridon@example.com",
    subject: "Tomorrow’s client visit",
    excerpt: "I’ve got the checklist. Are we meeting at the office?",
    body: "Agim,\n\nThe onboarding checklist is ready. Are we meeting at the office before heading to the client?\n\nLiridon",
    time: "10:32 AM",
    rank: 5,
    folder: "Inbox",
    label: "Work",
    unread: true,
    star: false,
  },
  {
    id: 5,
    name: "Arsim Mehmeti",
    email: "arsim@example.com",
    subject: "Weekend plans",
    excerpt: "Let’s catch up when you have a minute.",
    body: "Hey Agim,\n\nLet’s catch up over the weekend. Call me when you have a minute.\n\nArsim",
    time: "Yesterday",
    rank: 4,
    folder: "Inbox",
    label: "Personal",
    unread: false,
    star: false,
  },
  {
    id: 6,
    name: "Orbit",
    email: "billing@example.com",
    subject: "Your October invoice",
    excerpt: "Your workspace invoice is ready.",
    body: "Your October workspace invoice is attached.\n\nThank you for being part of Orbit.",
    time: "Yesterday",
    rank: 3,
    folder: "Archive",
    label: "Work",
    unread: false,
    star: false,
    file: "Invoice.pdf",
    size: "124 KB",
  },
  {
    id: 7,
    name: "Unknown sender",
    email: "offer@example.com",
    subject: "A special offer for you",
    excerpt: "An unsolicited offer moved to spam.",
    body: "This sample message is in Spam.",
    time: "Monday",
    rank: 2,
    folder: "Spam",
    label: "Promotions",
    unread: false,
    star: false,
  },
  {
    id: 8,
    name: "You",
    email: "agim@example.com",
    to: "team@example.com",
    subject: "Launch plan follow-up",
    excerpt: "Thanks everyone. Here are the next steps.",
    body: "Thanks everyone. Here are the next steps for launch.",
    time: "Monday",
    rank: 1,
    folder: "Sent",
    label: "Work",
    unread: false,
    star: false,
  },
];
export default function MailApp() {
  const [messages, setMessages] = useStoredState("mail", samples),
    [folder, setFolder] = useState("Inbox"),
    [filter, setFilter] = useState("All"),
    [query, setQuery] = useState(""),
    [selected, setSelected] = useState(1),
    [checked, setChecked] = useState([]),
    [sort, setSort] = useState("Newest"),
    [compact, setCompact] = useState(false),
    [focus, setFocus] = useState(false),
    [compose, setCompose] = useState(null),
    [draftId, setDraftId] = useState(null),
    [fields, setFields] = useState({
      to: "",
      cc: "",
      bcc: "",
      subject: "",
      body: "",
      files: [],
    }),
    [cc, setCc] = useState(false),
    [error, setError] = useState(""),
    [panel, setPanel] = useState(null),
    [status, setStatus] = useState("Sample mailbox · saved in this browser"),
    [undo, setUndo] = useState(null);

  const I = (name) => h("i", { "data-lucide": name, "aria-hidden": true });
  const B = (text, fn, icon, cls = "", extra = {}) =>
    h(
      "button",
      {
        type: "button",
        onClick: fn,
        className: "cursor-interaction " + cls,
        ...extra,
      },
      icon ? I(icon) : null,
      text,
    );
  const patch = (ids, changes) =>
    setMessages((prev) =>
      prev.map((m) => (ids.includes(m.id) ? { ...m, ...changes } : m)),
    );
  const here = (m) =>
    folder === "Starred"
      ? m.star && m.folder !== "Trash"
      : ["Work", "Personal", "Promotions"].includes(folder)
        ? m.label === folder && !["Trash", "Spam"].includes(m.folder)
        : m.folder === folder;
  const visible = messages
    .filter(
      (m) =>
        here(m) &&
        (filter !== "Unread" || m.unread) &&
        [m.name, m.subject, m.body]
          .join(" ")
          .toLowerCase()
          .includes(query.toLowerCase()),
    )
    .sort((a, b) => (sort === "Newest" ? b.rank - a.rank : a.rank - b.rank));
  const current = visible.find((m) => m.id === selected) || visible[0];
  function go(f) {
    setCompose(null);
    setFolder(f);
    setChecked([]);
    setPanel(null);
    setFilter("All");
    setQuery("");
    setFocus(false);
  }
  function move(ids, dest) {
    setUndo(messages);
    patch(ids, { folder: dest });
    setChecked([]);
    setPanel(null);
    setStatus(
      ids.length +
        " conversation" +
        (ids.length === 1 ? "" : "s") +
        " moved to " +
        dest,
    );
  }
  function start(mode, m) {
    setDraftId(mode === "draft" ? m.id : null);
    setError("");
    setCc(false);
    setPanel(null);
    setCompose(mode);
    setFields(
      mode === "new"
        ? { to: "", cc: "", bcc: "", subject: "", body: "", files: [] }
        : mode === "draft"
          ? {
              to: m.to || "",
              cc: m.cc || "",
              bcc: m.bcc || "",
              subject: m.subject,
              body: m.body,
              files: m.files || [],
            }
          : {
              to: mode === "forward" ? "" : m.email,
              cc: mode === "replyall" ? "team@example.com" : "",
              bcc: "",
              subject: (mode === "forward" ? "Fwd: " : "Re: ") + m.subject,
              body:
                mode === "forward"
                  ? "\n\n—— Forwarded message ——\n" + m.body
                  : "",
              files: mode === "forward" && m.file ? [m.file] : [],
            },
    );
    if (mode === "replyall") setCc(true);
  }
  function finish(dest) {
    if (
      dest === "Sent" &&
      (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(fields.to) ||
        !fields.subject.trim() ||
        !fields.body.trim())
    ) {
      setError("Add a valid recipient, subject, and message.");
      return;
    }
    const entry = {
      id: Date.now(),
      rank: Date.now(),
      name: "You",
      email: "agim@example.com",
      ...fields,
      excerpt: fields.body.slice(0, 90),
      time: "Just now",
      folder: dest,
      label: "Work",
      unread: false,
      star: false,
      file: fields.files[0],
    };
    setUndo(messages);
    setMessages((prev) =>
      compose === "draft" && draftId
        ? prev.filter((m) => m.id !== draftId).concat(entry)
        : prev.concat(entry),
    );
    setCompose(null);
    setStatus(
      dest === "Sent"
        ? "Message added to Sent · demo only"
        : "Draft saved in this browser",
    );
  }
  const icons = {
    Inbox: "inbox",
    Starred: "star",
    Snoozed: "clock",
    Sent: "send",
    Drafts: "file-pen-line",
    Archive: "archive",
    Spam: "shield-alert",
    Trash: "trash-2",
  };
  const nav = (f) =>
    B(
      h(
        React.Fragment,
        null,
        f,
        h(
          "span",
          { className: "count" },
          messages.filter((m) =>
            f === "Starred" ? m.star && m.folder !== "Trash" : m.folder === f,
          ).length || "",
        ),
      ),
      () => go(f),
      icons[f],
      "nav " + (folder === f ? "active" : ""),
      { key: f, "aria-pressed": folder === f },
    );
  const tool = (label, fn, icon, cls = "") =>
    B(null, fn, icon, "tool " + cls, {
      "aria-label": label,
      "data-tooltip": label,
    });
  const field = (name, label, type = "text") =>
    h(
      "label",
      null,
      label,
      h("input", {
        type,
        value: fields[name],
        onChange: (e) => setFields({ ...fields, [name]: e.target.value }),
      }),
    );
  const composer = compose
    ? h(
        "section",
        { className: "composebox", "aria-label": "Compose message" },
        h(
          "div",
          { className: "row between" },
          h(
            "strong",
            null,
            compose === "new"
              ? "New message"
              : compose === "forward"
                ? "Forward message"
                : compose === "draft"
                  ? "Edit draft"
                  : "Reply",
          ),
          tool(
            "Discard composition",
            () => {
              setCompose(null);
              setError("");
            },
            "x",
          ),
        ),
        field("to", "To", "email"),
        B(cc ? "Hide Cc / Bcc" : "Cc / Bcc", () => setCc(!cc), null, "small"),
        cc
          ? h(React.Fragment, null, field("cc", "Cc"), field("bcc", "Bcc"))
          : null,
        field("subject", "Subject"),
        h(
          "label",
          null,
          "Message",
          h("textarea", {
            value: fields.body,
            onChange: (e) => setFields({ ...fields, body: e.target.value }),
            placeholder: "Write something worth sending…",
          }),
        ),
        h(
          "label",
          { className: "small" },
          "Attachments",
          h("input", {
            type: "file",
            multiple: true,
            onChange: (e) =>
              setFields({
                ...fields,
                files: [
                  ...fields.files,
                  ...Array.from(e.target.files).map((f) => f.name),
                ],
              }),
          }),
        ),
        fields.files.map((name, i) =>
          h(
            "div",
            { key: i, className: "row small" },
            I("paperclip"),
            name,
            B(
              "Remove",
              () =>
                setFields({
                  ...fields,
                  files: fields.files.filter((_, j) => i !== j),
                }),
              null,
              "small",
            ),
          ),
        ),
        error ? h("p", { className: "error", role: "alert" }, error) : null,
        h(
          "div",
          { className: "row" },
          B("Send", () => finish("Sent"), "send", "mainaction"),
          B("Save draft", () => finish("Drafts"), "file-pen-line"),
        ),
      )
    : null;
  return h(
    React.Fragment,
    null,
    h(
      "header",
      { className: "top" },
      h(
        "div",
        { className: "brand" },
        h("b", null, "t"),
        "thura",
        h("small", null, "Mail"),
      ),
      h(
        "div",
        { className: "account" },
        h("span", { className: "accountname" }, "Sample workspace"),
        h("span", { className: "avatar" }, "DU"),
      ),
    ),
    h(
      "div",
      { className: "layout " + (focus ? "focuslayout" : "") },
      h(
        "aside",
        { className: "folders" },
        B("Compose", () => start("new"), "square-pen", "mainaction compose"),
        h("nav", { "aria-label": "Mail folders" }, Object.keys(icons).map(nav)),
        h("div", { className: "eyebrow" }, "Labels"),
        h(
          "nav",
          { "aria-label": "Mail labels" },
          ["Work", "Personal", "Promotions"].map((label) =>
            B(
              h(
                React.Fragment,
                null,
                h("span", { className: "labeldot " + label.toLowerCase() }),
                label,
              ),
              () => go(label),
              null,
              "nav " + (folder === label ? "active" : ""),
              { key: label, "aria-pressed": folder === label },
            ),
          ),
        ),
      ),
      h(
        "section",
        { className: "list", "aria-label": "Conversations" },
        h(
          "div",
          { className: "listhead" },
          h(
            "div",
            { className: "row between" },
            h("h2", null, folder),
            h(
              "span",
              { className: "small quiet" },
              visible.length + " conversations",
            ),
          ),
          h(
            "label",
            { className: "search" },
            I("search"),
            h("input", {
              value: query,
              onChange: (e) => {
                setQuery(e.target.value);
                setChecked([]);
              },
              placeholder: "Search mail",
              "aria-label": "Search mail",
            }),
          ),
          h(
            "div",
            { className: "filterbar" },
            h(
              "div",
              null,
              ["All", "Unread"].map((f) =>
                B(
                  f,
                  () => {
                    setFilter(f);
                    setChecked([]);
                  },
                  null,
                  f === filter ? "active" : "",
                  { key: f, "aria-pressed": f === filter },
                ),
              ),
            ),
            h(
              "select",
              {
                value: sort,
                onChange: (e) => setSort(e.target.value),
                "aria-label": "Sort conversations",
              },
              h("option", null, "Newest"),
              h("option", null, "Oldest"),
            ),
          ),
        ),
        h(
          "div",
          { className: "bulk" },
          h(
            "label",
            { className: "small quiet" },
            h("input", {
              type: "checkbox",
              "aria-label": "Select all conversations",
              checked:
                visible.length > 0 &&
                visible.every((m) => checked.includes(m.id)),
              onChange: (e) =>
                setChecked(e.target.checked ? visible.map((m) => m.id) : []),
            }),
            checked.length ? checked.length + " selected" : "Select",
          ),
          checked.length
            ? h(
                React.Fragment,
                null,
                tool(
                  "Archive selected",
                  () => move(checked, "Archive"),
                  "archive",
                ),
                tool(
                  "Mark selected unread",
                  () => {
                    patch(checked, { unread: true });
                    setStatus("Selected conversations marked unread");
                  },
                  "mail",
                ),
                tool(
                  "Move selected to trash",
                  () => move(checked, "Trash"),
                  "trash-2",
                ),
              )
            : h(
                "label",
                { className: "dense-label" },
                h("input", {
                  type: "checkbox",
                  checked: compact,
                  onChange: (e) => setCompact(e.target.checked),
                }),
                "Compact",
              ),
        ),
        visible.length
          ? visible.map((m) =>
              h(
                "div",
                {
                  key: m.id,
                  className:
                    "item " +
                    (current?.id === m.id ? "selected " : "") +
                    (m.unread ? "unread " : "") +
                    (compact ? "compact" : ""),
                },
                h("input", {
                  type: "checkbox",
                  "aria-label": "Select " + m.subject,
                  checked: checked.includes(m.id),
                  onChange: (e) =>
                    setChecked(
                      e.target.checked
                        ? [...checked, m.id]
                        : checked.filter((id) => id !== m.id),
                    ),
                }),
                B(
                  h(
                    React.Fragment,
                    null,
                    h(
                      "div",
                      { className: "sender" },
                      h("span", { className: "sendername" }, m.name),
                      h("span", { className: "small quiet" }, m.time),
                    ),
                    h("div", { className: "subject" }, m.subject),
                    !compact
                      ? h("div", { className: "excerpt" }, m.excerpt)
                      : null,
                    h(
                      "div",
                      { className: "badge " + m.label.toLowerCase() },
                      m.file ? I("paperclip") : null,
                      m.label,
                    ),
                  ),
                  () => {
                    setSelected(m.id);
                    patch([m.id], { unread: false });
                    setPanel(null);
                    setCompose(null);
                  },
                  null,
                  "openmail",
                ),
                B(
                  null,
                  () => patch([m.id], { star: !m.star }),
                  "star",
                  "star " + (m.star ? "on" : ""),
                  {
                    "aria-label": (m.star ? "Unstar " : "Star ") + m.subject,
                    "aria-pressed": m.star,
                  },
                ),
              ),
            )
          : h("p", { className: "empty" }, "No conversations match this view."),
      ),
      h(
        "article",
        { className: "reader", "aria-label": "Reading pane" },
        h(
          "div",
          { className: "toolbar" },
          current
            ? h(
                React.Fragment,
                null,
                tool(
                  "Archive conversation",
                  () => move([current.id], "Archive"),
                  "archive",
                ),
                tool(
                  "Move to trash",
                  () => move([current.id], "Trash"),
                  "trash-2",
                  "delete",
                ),
                tool(
                  "Mark unread",
                  () => {
                    patch([current.id], { unread: true });
                    setStatus("Marked unread");
                  },
                  "mail",
                ),
                tool(
                  "Snooze",
                  () => setPanel(panel === "snooze" ? null : "snooze"),
                  "clock",
                ),
                tool(
                  "Change label",
                  () => setPanel(panel === "labels" ? null : "labels"),
                  "tag",
                ),
              )
            : null,
          h("span", { className: "grow" }),
          tool(
            focus ? "Show conversation list" : "Expand reading pane",
            () => setFocus(!focus),
            focus ? "panel-left-open" : "maximize-2",
          ),
        ),
        composer ||
          h(
            React.Fragment,
            null,
            current
              ? h(
                  React.Fragment,
                  null,
                  panel === "snooze"
                    ? h(
                        "div",
                        { className: "inlinepanel" },
                        h("strong", null, "Snooze until"),
                        h(
                          "div",
                          { className: "row" },
                          ["Later today", "Tomorrow", "Next week"].map((t) =>
                            B(
                              t,
                              () => {
                                patch([current.id], { snooze: t });
                                move([current.id], "Snoozed");
                                setStatus(
                                  "Snoozed until " +
                                    t +
                                    " · no real reminder is scheduled",
                                );
                              },
                              "clock",
                              "small",
                              { key: t },
                            ),
                          ),
                        ),
                      )
                    : null,
                  panel === "labels"
                    ? h(
                        "div",
                        { className: "inlinepanel" },
                        h("strong", null, "Apply label"),
                        h(
                          "div",
                          { className: "row" },
                          ["Work", "Personal", "Promotions"].map((t) =>
                            B(
                              t,
                              () => {
                                patch([current.id], { label: t });
                                setPanel(null);
                                setStatus("Label changed to " + t);
                              },
                              "tag",
                              "small",
                              { key: t },
                            ),
                          ),
                        ),
                      )
                    : null,
                  h(
                    "div",
                    { className: "row" },
                    h(
                      "span",
                      { className: "badge " + current.label.toLowerCase() },
                      current.label,
                    ),
                    current.folder === "Snoozed"
                      ? h(
                          "span",
                          { className: "small quiet" },
                          "Until " + current.snooze,
                        )
                      : null,
                  ),
                  h("h3", { className: "title" }, current.subject),
                  h(
                    "div",
                    { className: "row between" },
                    h(
                      "div",
                      { className: "row" },
                      h(
                        "span",
                        { className: "avatar" },
                        current.name
                          .split(" ")
                          .map((n) => n[0])
                          .join(""),
                      ),
                      h(
                        "div",
                        null,
                        h("strong", null, current.name),
                        h("div", { className: "small quiet" }, current.email),
                        h(
                          "div",
                          { className: "small quiet" },
                          "To " + (current.to || "you"),
                        ),
                      ),
                    ),
                    B(
                      null,
                      () => patch([current.id], { star: !current.star }),
                      "star",
                      "star " + (current.star ? "on" : ""),
                      {
                        "aria-label": "Star current conversation",
                        "aria-pressed": current.star,
                      },
                    ),
                  ),
                  h("div", { className: "message" }, current.body),
                  current.file
                    ? B(
                        h(
                          React.Fragment,
                          null,
                          h("span", { className: "fileicon" }, I("file-text")),
                          h(
                            "span",
                            null,
                            h("strong", null, current.file),
                            h(
                              "div",
                              { className: "small quiet" },
                              current.size || "Local demo attachment",
                            ),
                          ),
                          h("span", { className: "small quiet" }, "Preview"),
                        ),
                        () => setPanel(panel === "file" ? null : "file"),
                        null,
                        "attachment",
                      )
                    : null,
                  panel === "file"
                    ? h(
                        "div",
                        { className: "inlinepanel" },
                        h("strong", null, current.file),
                        h(
                          "p",
                          { className: "small quiet" },
                          "Sample preview — the original file contents are not loaded.",
                        ),
                        h(
                          "p",
                          null,
                          "Website launch / milestones and final checklist",
                        ),
                        B("Close preview", () => setPanel(null), "x"),
                      )
                    : null,
                  current.id === 1
                    ? h(
                        "div",
                        { className: "linkevent" },
                        h(
                          "div",
                          { className: "row" },
                          I("calendar-days"),
                          h("strong", null, "Website launch review"),
                        ),
                        h(
                          "p",
                          { className: "small" },
                          "Today · 2:30–3:00 PM · America/New_York",
                        ),
                        B(
                          "View event",
                          () => setPanel(panel === "event" ? null : "event"),
                          "arrow-up-right",
                        ),
                        panel === "event"
                          ? h(
                              "p",
                              { className: "small" },
                              "Sarah Chen · Marcus Williams · Agim Mehmeti. Agenda: approve assets and confirm launch timing. Sample event only.",
                            )
                          : null,
                      )
                    : null,
                  h(
                    "div",
                    { className: "row replybar" },
                    current.folder === "Drafts"
                      ? B(
                          "Edit draft",
                          () => start("draft", current),
                          "square-pen",
                          "mainaction",
                        )
                      : h(
                          React.Fragment,
                          null,
                          B("Reply", () => start("reply", current), "reply"),
                          B(
                            "Reply all",
                            () => start("replyall", current),
                            "reply-all",
                          ),
                          B(
                            "Forward",
                            () => start("forward", current),
                            "forward",
                          ),
                        ),
                    ["Trash", "Spam", "Snoozed"].includes(current.folder)
                      ? B(
                          "Move to Inbox",
                          () => move([current.id], "Inbox"),
                          "inbox",
                        )
                      : null,
                  ),
                )
              : h(
                  "div",
                  { className: "empty" },
                  "Choose a conversation or compose a new message.",
                ),
          ),
      ),
    ),
    h(
      "footer",
      { className: "footer" },
      h("span", { "aria-live": "polite" }, status),
      undo
        ? B(
            "Undo",
            () => {
              setMessages(undo);
              setUndo(null);
              setStatus("Last move, save, or send undone");
            },
            "undo-2",
            "small",
          )
        : h("span", null, "React · sample data"),
    ),
  );
}
