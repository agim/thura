import { useState, useEffect } from "react";
import { h, icon, btn, avatar, search } from "../lib/ui.js";
import { useStoredState } from "../lib/storage.js";

export default function Chat({ notice, requested }) {
  const [channels, setChannels] = useStoredState("chat-channels", [
    {
      id: "launch",
      name: "Website launch",
      sub: "Sarah, Marcus, you",
      kind: "group",
    },
    {
      id: "team",
      name: "CCN team",
      sub: "Operations & everyday updates",
      kind: "group",
    },
    {
      id: "sarah",
      name: "Sarah Chen",
      sub: "Creative director",
      kind: "person",
    },
    {
      id: "liridon",
      name: "Liridon Shabani",
      sub: "IT operations",
      kind: "person",
    },
  ]);
  const [active, setActive] = useState("launch"),
    [query, setQuery] = useState(""),
    [draft, setDraft] = useState(""),
    [showPeople, setShowPeople] = useState(false),
    [newName, setNewName] = useState(""),
    [creating, setCreating] = useState(false);
  const [messages, setMessages] = useStoredState("chat-messages", {
    launch: [
      {
        id: 1,
        from: "Sarah Chen",
        text: "Morning! The launch brief is ready. I’ve tightened the copy and added the new milestones.",
        time: "10:24 AM",
      },
      {
        id: 2,
        from: "Marcus Williams",
        text: "The coral and warm neutrals look great together. Brand assets are ready for the final review.",
        time: "10:28 AM",
        attachment: "Brand assets.zip",
      },
      {
        id: 3,
        from: "You",
        text: "Love this direction. Let’s review at 2:30 and lock the launch checklist.",
        time: "10:31 AM",
      },
      {
        id: 4,
        from: "Sarah Chen",
        text: "Perfect. I’ll bring the updated brief.",
        time: "10:32 AM",
        likes: 2,
      },
    ],
    team: [
      {
        id: 5,
        from: "Liridon Shabani",
        text: "The onboarding checklist is updated. Ready for tomorrow’s client visit.",
        time: "9:15 AM",
      },
    ],
    sarah: [
      {
        id: 6,
        from: "Sarah Chen",
        text: "Hey Agim! Let me know when you’ve had a chance to look through the brief.",
        time: "10:12 AM",
      },
    ],
    liridon: [
      {
        id: 7,
        from: "Liridon Shabani",
        text: "I can meet you at the office before the client visit.",
        time: "9:45 AM",
      },
    ],
  });
  useEffect(() => {
    if (!requested) return;
    const id = "person-" + requested.id;
    setChannels((c) =>
      c.some((x) => x.id === id)
        ? c
        : [
            ...c,
            { id, name: requested.name, sub: requested.role, kind: "person" },
          ],
    );
    setActive(id);
    setDraft("");
  }, [requested]);
  const current = channels.find((c) => c.id === active) || channels[0];
  function send() {
    if (!draft.trim()) return;
    setMessages((m) => ({
      ...m,
      [active]: [
        ...(m[active] || []),
        { id: Date.now(), from: "You", text: draft.trim(), time: "Just now" },
      ],
    }));
    setDraft("");
    notice("Message added to this demo conversation");
  }
  function create() {
    if (!newName.trim()) return;
    const id = "channel-" + Date.now();
    setChannels((c) => [
      ...c,
      { id, name: newName.trim(), sub: "New conversation", kind: "group" },
    ]);
    setActive(id);
    setCreating(false);
    setNewName("");
    notice("Conversation created");
  }
  return h(
    "div",
    { className: "ws-page" },
    h(
      "div",
      { className: "pagehead" },
      h(
        "div",
        null,
        h("h2", null, "Chat"),
        h("p", { className: "sub" }, "Good conversations. Great work."),
      ),
      btn(
        "New conversation",
        () => setCreating(!creating),
        "square-pen",
        "primary",
      ),
    ),
    h(
      "div",
      { className: "split" },
      h(
        "aside",
        { className: "side" },
        search(query, setQuery, "Find a conversation"),
        creating
          ? h(
              "div",
              { className: "newchat" },
              h(
                "label",
                null,
                "Conversation name",
                h("input", {
                  value: newName,
                  onChange: (e) => setNewName(e.target.value),
                }),
              ),
              btn("Create", create, "plus", "primary"),
            )
          : null,
        h("div", { className: "sectionlabel" }, "Conversations"),
        h(
          "div",
          { className: "channel-list" },
          channels
            .filter((c) => c.name.toLowerCase().includes(query.toLowerCase()))
            .map((c) =>
              btn(
                h(
                  "div",
                  null,
                  h(
                    "div",
                    { className: "flex" },
                    icon(c.kind === "group" ? "hash" : "user"),
                    h("strong", null, c.name),
                  ),
                  h("div", { className: "sub" }, c.sub),
                ),
                () => {
                  setActive(c.id);
                  setShowPeople(false);
                },
                null,
                "chat-channel " + (active === c.id ? "active" : ""),
                { key: c.id, "aria-pressed": active === c.id },
              ),
            ),
        ),
      ),
      h(
        "section",
        null,
        h(
          "div",
          { className: "channelhead flex spread" },
          h(
            "div",
            null,
            h("h3", null, current.name),
            h("div", { className: "sub" }, current.sub),
          ),
          btn(
            showPeople ? "Hide details" : "Details",
            () => setShowPeople(!showPeople),
            "users",
            "secondary",
          ),
        ),
        showPeople
          ? h(
              "div",
              { className: "detail-panel" },
              h("strong", null, current.name),
              h("p", { className: "tiny" }, current.sub),
              h(
                "p",
                { className: "tiny muted" },
                "Notifications on · Conversation history saved in this browser.",
              ),
            )
          : null,
        h(
          "div",
          { className: "thread", "aria-live": "polite" },
          h("div", { className: "date-divider" }, "TUESDAY, OCTOBER 6"),
          (messages[active] || []).map((m) =>
            h(
              "div",
              {
                key: m.id,
                className: "chatline " + (m.from === "You" ? "mine" : ""),
              },
              avatar(
                m.from,
                m.from === "Marcus Williams"
                  ? "gold"
                  : m.from === "You"
                    ? "coral"
                    : "",
              ),
              h(
                "div",
                { className: "grow" },
                h(
                  "div",
                  { className: "flex" },
                  h("strong", { className: "tiny" }, m.from),
                  h("span", { className: "tiny muted" }, m.time),
                ),
                h(
                  "div",
                  { className: "bubble" },
                  m.text,
                  m.attachment
                    ? h(
                        "div",
                        { className: "flex", style: { marginTop: 10 } },
                        icon("paperclip"),
                        h("strong", { className: "tiny" }, m.attachment),
                      )
                    : null,
                ),
                btn(
                  m.likes || 0 ? "Like · " + m.likes : "Like",
                  () =>
                    setMessages((all) => ({
                      ...all,
                      [active]: all[active].map((x) =>
                        x.id === m.id
                          ? {
                              ...x,
                              liked: !x.liked,
                              likes: (x.likes || 0) + (x.liked ? -1 : 1),
                            }
                          : x,
                      ),
                    })),
                  "thumbs-up",
                  "reaction",
                  { "aria-pressed": !!m.liked },
                ),
              ),
            ),
          ),
          !(messages[active] || []).length
            ? h("div", { className: "empty-space" }, "Start the conversation.")
            : null,
        ),
        h(
          "form",
          {
            className: "chat-reply",
            onSubmit: (e) => {
              e.preventDefault();
              send();
            },
          },
          h(
            "div",
            { className: "composerbox" },
            h("textarea", {
              value: draft,
              onChange: (e) => setDraft(e.target.value),
              placeholder: "Message " + current.name,
              "aria-label": "Message " + current.name,
              onKeyDown: (e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  send();
                }
              },
            }),
            h(
              "div",
              { className: "flex spread" },
              h(
                "span",
                { className: "tiny muted" },
                "Enter to send · Shift + Enter for a new line",
              ),
              btn("Send", send, "send", "primary", { disabled: !draft.trim() }),
            ),
          ),
        ),
      ),
    ),
  );
}
