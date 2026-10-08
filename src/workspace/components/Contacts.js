import React, { useState } from "react";
import { h, icon, btn, avatar, search } from "../lib/ui.js";
import { useStoredState } from "../lib/storage.js";

const peopleSeed = [
  {
    id: 1,
    name: "Sarah Chen",
    role: "Creative director",
    company: "Studio North",
    email: "sarah@example.com",
    phone: "(410) 555-0142",
    group: "Collaborators",
    color: "",
    favorite: true,
  },
  {
    id: 2,
    name: "Marcus Williams",
    role: "Brand designer",
    company: "Studio North",
    email: "marcus@example.com",
    phone: "(410) 555-0168",
    group: "Collaborators",
    color: "gold",
    favorite: false,
  },
  {
    id: 3,
    name: "Liridon Shabani",
    role: "IT operations",
    company: "Charm City Networks",
    email: "liridon@example.com",
    phone: "(410) 555-0124",
    group: "Team",
    color: "coral",
    favorite: true,
  },
  {
    id: 4,
    name: "Arsim Mehmeti",
    role: "Personal contact",
    company: "London",
    email: "arsim@example.com",
    phone: "",
    group: "Personal",
    color: "gold",
    favorite: false,
  },
  {
    id: 5,
    name: "Nora Wilson",
    role: "Client success",
    company: "Charm City Networks",
    email: "nora@example.com",
    phone: "(410) 555-0182",
    group: "Team",
    color: "",
    favorite: false,
  },
  {
    id: 6,
    name: "Daniel Brooks",
    role: "Project manager",
    company: "Harbor House",
    email: "daniel@example.com",
    phone: "(410) 555-0193",
    group: "Clients",
    color: "coral",
    favorite: false,
  },
];

export default function Contacts({ notice, openChat }) {
  const [people, setPeople] = useStoredState("contacts", peopleSeed),
    [query, setQuery] = useState(""),
    [group, setGroup] = useState("All contacts"),
    [selected, setSelected] = useState(1),
    [edit, setEdit] = useState(null),
    [error, setError] = useState("");
  const visible = people.filter(
    (p) =>
      (group === "All contacts" ||
        (group === "Favorites" && p.favorite) ||
        p.group === group) &&
      [p.name, p.company, p.email]
        .join(" ")
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  const current = people.find((p) => p.id === selected);
  function save() {
    if (!edit.name.trim() || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(edit.email)) {
      setError("Add a name and valid email address.");
      return;
    }
    const saved = { ...edit, id: edit.id || Date.now() };
    setPeople((ps) => [...ps.filter((p) => p.id !== saved.id), saved]);
    setSelected(saved.id);
    setEdit(null);
    setGroup("All contacts");
    setQuery("");
    notice("Contact saved");
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
        h("h2", null, "Contacts"),
        h("p", { className: "sub" }, "The people behind the work."),
      ),
      btn(
        "Add contact",
        () => {
          setEdit({
            name: "",
            role: "",
            company: "",
            email: "",
            phone: "",
            group: "Team",
            color: "",
            favorite: false,
          });
          setError("");
        },
        "user-plus",
        "primary",
      ),
    ),
    edit
      ? h(
          "section",
          { className: "inline-form" },
          h(
            "div",
            { className: "flex spread" },
            h("h3", null, edit.id ? "Edit contact" : "New contact"),
            btn("Close", () => setEdit(null), "x"),
          ),
          h(
            "div",
            { className: "form-grid" },
            [
              ["name", "Full name"],
              ["role", "Role"],
              ["company", "Company"],
              ["email", "Email"],
              ["phone", "Phone"],
            ].map(([key, label]) =>
              h(
                "label",
                { key },
                label,
                h("input", {
                  value: edit[key],
                  type:
                    key === "email"
                      ? "email"
                      : key === "phone"
                        ? "tel"
                        : "text",
                  onChange: (e) => setEdit({ ...edit, [key]: e.target.value }),
                }),
              ),
            ),
            h(
              "label",
              null,
              "Group",
              h(
                "select",
                {
                  value: edit.group,
                  onChange: (e) => setEdit({ ...edit, group: e.target.value }),
                },
                ["Team", "Collaborators", "Clients", "Personal"].map((g) =>
                  h("option", { key: g }, g),
                ),
              ),
            ),
          ),
          error ? h("p", { className: "error", role: "alert" }, error) : null,
          btn("Save contact", save, "check", "primary"),
        )
      : null,
    h(
      "div",
      { className: "flex spread", style: { padding: "0 24px 20px" } },
      search(query, setQuery, "Search people"),
      h(
        "select",
        {
          "aria-label": "Contact group",
          value: group,
          onChange: (e) => setGroup(e.target.value),
        },
        [
          "All contacts",
          "Favorites",
          "Team",
          "Collaborators",
          "Clients",
          "Personal",
        ].map((g) => h("option", { key: g }, g)),
      ),
    ),
    h(
      "div",
      { className: "contact-layout" },
      h(
        "section",
        { className: "contact-list", "aria-label": "Contact list" },
        visible.map((p) =>
          btn(
            h(
              React.Fragment,
              null,
              h(
                "div",
                { className: "flex spread" },
                avatar(p.name, p.color),
                p.favorite ? icon("star") : null,
              ),
              h("h3", null, p.name),
              h("div", { className: "sub" }, p.role),
              h("div", { className: "sub" }, p.company),
              h(
                "span",
                {
                  className:
                    "tag " +
                    (p.group === "Personal"
                      ? "coral"
                      : p.group === "Collaborators"
                        ? "gold"
                        : ""),
                  style: { marginTop: 14 },
                },
                p.group,
              ),
            ),
            () => setSelected(p.id),
            null,
            "contact-card " + (selected === p.id ? "active" : ""),
            { key: p.id, "aria-pressed": selected === p.id },
          ),
        ),
        !visible.length
          ? h("div", { className: "empty-space" }, "No matching contacts.")
          : null,
      ),
      current
        ? h(
            "aside",
            { className: "contact-detail" },
            h(
              "div",
              null,
              avatar(current.name, "large " + current.color),
              h("h3", null, current.name),
              h("p", { className: "sub" }, current.role),
              h(
                "div",
                { className: "flex", style: { marginTop: 18 } },
                btn(
                  "Message",
                  () => openChat(current),
                  "message-circle",
                  "primary",
                ),
                btn(
                  null,
                  () => {
                    setPeople((ps) =>
                      ps.map((p) =>
                        p.id === current.id
                          ? { ...p, favorite: !p.favorite }
                          : p,
                      ),
                    );
                  },
                  "star",
                  "secondary",
                  {
                    "aria-label": current.favorite
                      ? "Remove favorite"
                      : "Add favorite",
                    "aria-pressed": !!current.favorite,
                  },
                ),
              ),
            ),
            h(
              "dl",
              null,
              h("dt", null, "Email"),
              h("dd", null, current.email),
              h("dt", null, "Phone"),
              h("dd", null, current.phone || "Not added"),
              h("dt", null, "Company"),
              h("dd", null, current.company || "Not added"),
              h("dt", null, "Group"),
              h("dd", null, current.group),
            ),
            btn(
              "Edit contact",
              () => {
                setEdit({ ...current });
                setError("");
              },
              "square-pen",
              "secondary",
            ),
          )
        : null,
    ),
  );
}
