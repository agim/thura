import React, { useState, useEffect } from "react";
import { h, btn, avatar } from "./lib/ui.js";
import { useStoredState } from "./lib/storage.js";
import MailApp from "./components/Mail.js";
import Chat from "./components/Chat.js";
import Calendar from "./components/Calendar.js";
import Drive from "./components/Drive.js";
import Contacts from "./components/Contacts.js";
import Meet from "./components/Meet.js";
export default function Workspace() {
  const [app, setApp] = useStoredState("active-app", "Mail"),
    [status, setStatus] = useState("Sample workspace · saved in this browser"),
    [requested, setRequested] = useState(null);

  const [theme, setTheme] = useStoredState("theme", "system");
  useEffect(() => {
    const previous = document.documentElement.style.colorScheme;
    document.documentElement.style.colorScheme =
      theme === "system" ? "light dark" : theme;
    return () => { document.documentElement.style.colorScheme = previous; };
  }, [theme]);
  const [meeting, setMeeting] = useState(null);
  const tabs = [
    ["Mail", "mail"],
    ["Chat", "messages-square"],
    ["Calendar", "calendar-days"],
    ["Drive", "hard-drive"],
    ["Contacts", "contact-round"],
    ["Meet", "video"],
  ];
  return h(
    React.Fragment,
    null,
    h(
      "header",
      { className: "ws-top" },
      h(
        "div",
        { className: "ws-brand" },
        h("span", { className: "ws-logo" }, "t"),
        "thura",
        h("span", { className: "ws-org" }, "Sample workspace"),
      ),
      h(
        "div",
        { className: "ws-account" },
      h("a", { href: "/contacts" }, "Sign in"),
        h("span", { className: "tiny muted" }, "Your workspace, together"),
        avatar("Demo User", "coral"),
      ),
    ),
    h(
      "nav",
      { className: "ws-nav", "aria-label": "Workspace applications" },
      tabs.map(([name, i]) =>
        btn(
          h(React.Fragment, null, name),
          () => {
            setApp(name);
            setStatus("Sample workspace · saved in this browser");
          },
          i,
          app === name ? "active" : "",
          { key: name, "aria-current": app === name ? "page" : undefined },
        ),
      ),
      btn(
        theme === "system" ? "Auto" : theme === "light" ? "Light" : "Dark",
        () =>
          setTheme(
            theme === "system"
              ? "light"
              : theme === "light"
                ? "dark"
                : "system",
          ),
        theme === "dark" ? "moon" : theme === "light" ? "sun" : "monitor",
        "theme-control",
        { "aria-label": "Theme: " + theme + ". Click to change" },
      ),
    ),
    h(
      "div",
      { className: "ws-view", hidden: app !== "Mail" },
      h("div", { id: "orbit-mail-vibrant" }, h(MailApp)),
    ),
    h(
      "div",
      { className: "ws-view", hidden: app !== "Chat" },
      h(Chat, { notice: setStatus, requested }),
    ),
    h(
      "div",
      { className: "ws-view", hidden: app !== "Calendar" },
      h(Calendar, {
        notice: setStatus,
        openMeet: (event) => {
          setMeeting(event);
          setApp("Meet");
          setStatus("Meeting preview opened from Calendar");
        },
      }),
    ),
    h(
      "div",
      { className: "ws-view", hidden: app !== "Drive" },
      h(Drive, { notice: setStatus }),
    ),
    h(
      "div",
      { className: "ws-view", hidden: app !== "Contacts" },
      h(Contacts, {
        notice: setStatus,
        openChat: (person) => {
          setRequested({ ...person });
          setApp("Chat");
          setStatus("Conversation opened with " + person.name);
        },
      }),
    ),
    h(
      "div",
      { className: "ws-view", hidden: app !== "Meet" },
      h(Meet, { notice: setStatus, event: meeting }),
    ),
    h(
      "footer",
      { className: "ws-footer" },
      h("span", { "aria-live": "polite" }, status),
      h("span", null, "Thura / " + app),
    ),
  );
}
