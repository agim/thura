import React, { useState } from "react";
import { h, icon, btn, search } from "../lib/ui.js";
import { useStoredState } from "../lib/storage.js";

const fileSeed = [
  {
    id: 1,
    name: "Launch brief.pdf",
    folder: "Projects",
    type: "Document",
    size: "1.8 MB",
    owner: "Sarah Chen",
    date: "Oct 6",
    star: true,
    shared: true,
  },
  {
    id: 2,
    name: "Brand assets.zip",
    folder: "Brand studio",
    type: "Archive",
    size: "8.2 MB",
    owner: "Marcus Williams",
    date: "Oct 6",
    shared: true,
  },
  {
    id: 3,
    name: "Q4 planning.xlsx",
    folder: "Operations",
    type: "Spreadsheet",
    size: "246 KB",
    owner: "You",
    date: "Oct 5",
    star: true,
  },
  {
    id: 4,
    name: "Homepage concepts.png",
    folder: "Brand studio",
    type: "Image",
    size: "3.4 MB",
    owner: "Marcus Williams",
    date: "Oct 5",
    shared: true,
  },
  {
    id: 5,
    name: "Client onboarding.pdf",
    folder: "Operations",
    type: "Document",
    size: "940 KB",
    owner: "Liridon Shabani",
    date: "Oct 4",
    shared: true,
  },
  {
    id: 6,
    name: "Project timeline.xlsx",
    folder: "Projects",
    type: "Spreadsheet",
    size: "186 KB",
    owner: "You",
    date: "Oct 3",
  },
];
const fileIcon = (f) =>
  h(
    "span",
    {
      className:
        "file-symbol " +
        (f.type === "Spreadsheet"
          ? "sheet"
          : f.type === "Image"
            ? "image"
            : ""),
    },
    icon(
      f.type === "Spreadsheet"
        ? "sheet"
        : f.type === "Image"
          ? "image"
          : f.type === "Archive"
            ? "file-archive"
            : "file-text",
    ),
  );
export default function Drive({ notice }) {
  const [files, setFiles] = useStoredState("files", fileSeed),
    [section, setSection] = useState("My Drive"),
    [folder, setFolder] = useState(null),
    [folders, setFolders] = useStoredState("folders", [
      "Projects",
      "Brand studio",
      "Operations",
    ]),
    [query, setQuery] = useState(""),
    [view, setView] = useState("List"),
    [selected, setSelected] = useState(null),
    [panel, setPanel] = useState(null),
    [newFolder, setNewFolder] = useState(""),
    [shareWith, setShareWith] = useState(""),
    [error, setError] = useState("");
  const visible = files.filter(
    (f) =>
      (section === "Trash" ? f.trash : !f.trash) &&
      (section !== "Starred" || f.star) &&
      (section !== "Shared with me" || f.shared) &&
      (!folder || f.folder === folder) &&
      f.name.toLowerCase().includes(query.toLowerCase()),
  );
  const current = files.find((f) => f.id === selected);
  function patch(id, values) {
    setFiles((fs) => fs.map((f) => (f.id === id ? { ...f, ...values } : f)));
  }
  function open(id) {
    setSelected(id);
    setPanel("preview");
    setError("");
  }
  function upload(list) {
    const added = Array.from(list).map((f, i) => ({
      id: Date.now() + i,
      name: f.name,
      folder: folder || "Projects",
      type: f.type.startsWith("image/")
        ? "Image"
        : f.name.endsWith(".xlsx")
          ? "Spreadsheet"
          : "Document",
      size: Math.round(f.size / 1024) + " KB",
      owner: "You",
      date: "Just now",
    }));
    setFiles((fs) => [...fs, ...added]);
    setPanel(null);
    notice(
      added.length +
        " file names added locally; file contents are not uploaded",
    );
  }
  function addFolder() {
    if (!newFolder.trim()) return;
    if (folders.includes(newFolder.trim())) {
      setError("That folder already exists.");
      return;
    }
    setFolders((fs) => [...fs, newFolder.trim()]);
    setFolder(newFolder.trim());
    setNewFolder("");
    setPanel(null);
    notice("Folder created");
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
        h("h2", null, "Drive"),
        h("p", { className: "sub" }, "A home for your next big thing."),
      ),
      h(
        "div",
        { className: "flex" },
        btn(
          "New folder",
          () => {
            setPanel("folder");
            setError("");
          },
          "folder-plus",
          "secondary",
        ),
        btn("Upload files", () => setPanel("upload"), "upload", "primary"),
      ),
    ),
    h(
      "div",
      { className: "drive-layout" },
      h(
        "aside",
        { className: "side" },
        [
          ["My Drive", "hard-drive"],
          ["Shared with me", "users"],
          ["Starred", "star"],
          ["Trash", "trash-2"],
        ].map(([s, i]) =>
          btn(
            s,
            () => {
              setSection(s);
              setFolder(null);
              setPanel(null);
            },
            i,
            "siderow " + (section === s ? "active" : ""),
            { key: s, "aria-pressed": section === s },
          ),
        ),
      ),
      h(
        "section",
        { className: "drive-main" },
        h(
          "div",
          { className: "flex spread" },
          h(
            "div",
            { className: "flex" },
            btn(section, () => setFolder(null), null),
            folder
              ? h(
                  React.Fragment,
                  null,
                  icon("chevron-right"),
                  h("strong", null, folder),
                )
              : null,
          ),
          h(
            "div",
            { className: "flex" },
            search(query, setQuery, "Search files"),
            btn(
              null,
              () => setView(view === "List" ? "Grid" : "List"),
              view === "List" ? "layout-grid" : "list",
              "secondary",
              { "aria-label": view === "List" ? "Show grid" : "Show list" },
            ),
          ),
        ),
        panel === "upload"
          ? h(
              "div",
              { className: "detail-panel", style: { margin: "18px 0" } },
              h(
                "div",
                { className: "flex spread" },
                h("h3", null, "Add files"),
                btn("Close", () => setPanel(null), "x"),
              ),
              h(
                "p",
                { className: "tiny muted", style: { margin: "8px 0" } },
                "This preview keeps file names only.",
              ),
              h("input", {
                type: "file",
                multiple: true,
                "aria-label": "Choose files",
                onChange: (e) => upload(e.target.files),
              }),
            )
          : null,
        panel === "folder"
          ? h(
              "div",
              { className: "detail-panel", style: { margin: "18px 0" } },
              h(
                "label",
                null,
                "Folder name",
                h("input", {
                  value: newFolder,
                  onChange: (e) => setNewFolder(e.target.value),
                  style: { margin: "8px" },
                }),
              ),
              h(
                "div",
                { className: "flex" },
                btn("Create folder", addFolder, "plus", "primary"),
                btn("Cancel", () => setPanel(null)),
              ),
              error
                ? h("p", { className: "error", role: "alert" }, error)
                : null,
            )
          : null,
        (panel === "preview" || panel === "share") && current
          ? h(
              "section",
              { className: "detail-panel", style: { margin: "18px 0" } },
              h(
                "div",
                { className: "flex spread" },
                h(
                  "div",
                  { className: "flex" },
                  fileIcon(current),
                  h(
                    "div",
                    null,
                    h("h3", null, current.name),
                    h(
                      "p",
                      { className: "tiny muted" },
                      current.type +
                        " · " +
                        current.size +
                        " · " +
                        current.owner,
                    ),
                  ),
                ),
                btn("Close", () => setPanel(null), "x"),
              ),
              panel === "preview"
                ? h(
                    React.Fragment,
                    null,
                    h(
                      "p",
                      { className: "tiny", style: { margin: "15px 0" } },
                      current.folder + " / " + current.name,
                    ),
                    h(
                      "p",
                      { className: "tiny muted" },
                      "Sample file details. Original file contents are not loaded.",
                    ),
                    h(
                      "div",
                      { className: "flex", style: { marginTop: 14 } },
                      btn(
                        current.star ? "Unstar" : "Star",
                        () => patch(current.id, { star: !current.star }),
                        "star",
                        "secondary",
                      ),
                      btn(
                        "Share",
                        () => {
                          setPanel("share");
                          setShareWith("");
                          setError("");
                        },
                        "share-2",
                        "secondary",
                      ),
                      btn(
                        current.trash ? "Restore" : "Move to trash",
                        () => {
                          patch(current.id, { trash: !current.trash });
                          setPanel(null);
                          notice(
                            current.trash
                              ? "File restored"
                              : "File moved to Trash",
                          );
                        },
                        current.trash ? "undo-2" : "trash-2",
                      ),
                    ),
                  )
                : h(
                    "div",
                    null,
                    h(
                      "p",
                      { className: "tiny", style: { margin: "14px 0" } },
                      "People with access: " +
                        (current.owner === "You"
                          ? "You"
                          : current.owner + " and you"),
                    ),
                    h(
                      "div",
                      { className: "flex" },
                      h("input", {
                        type: "email",
                        value: shareWith,
                        onChange: (e) => setShareWith(e.target.value),
                        placeholder: "name@example.com",
                        "aria-label": "Share with email",
                      }),
                      btn(
                        "Add viewer",
                        () => {
                          if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(shareWith)) {
                            setError("Enter a valid email address.");
                            return;
                          }
                          patch(current.id, {
                            shared: true,
                            viewers: [...(current.viewers || []), shareWith],
                          });
                          setShareWith("");
                          setError("");
                          notice(
                            "Viewer added locally; no invitation was sent",
                          );
                        },
                        "user-plus",
                        "primary",
                      ),
                    ),
                    (current.viewers || []).map((v) =>
                      h(
                        "p",
                        { key: v, className: "tiny", style: { marginTop: 8 } },
                        v + " · Viewer",
                      ),
                    ),
                    error
                      ? h("p", { className: "error", role: "alert" }, error)
                      : null,
                  ),
            )
          : null,
        section === "My Drive" && !folder && !query
          ? h(
              "div",
              { className: "folder-grid" },
              folders.map((f) =>
                btn(
                  h(
                    React.Fragment,
                    null,
                    h("span", { className: "folder-symbol" }, icon("folder")),
                    h("strong", null, f),
                    h(
                      "span",
                      { className: "tiny muted" },
                      files.filter((x) => x.folder === f && !x.trash).length +
                        " files",
                    ),
                  ),
                  () => {
                    setFolder(f);
                    setPanel(null);
                  },
                  null,
                  "folder-tile",
                  { key: f },
                ),
              ),
            )
          : null,
        h(
          "div",
          { className: "flex spread", style: { margin: "18px 0 6px" } },
          h("h3", null, folder || section === "My Drive" ? "Files" : section),
          h("span", { className: "tiny muted" }, visible.length + " items"),
        ),
        !visible.length
          ? h("div", { className: "empty-space" }, "No files in this view.")
          : view === "List"
            ? h(
                "table",
                { className: "files" },
                h(
                  "thead",
                  null,
                  h(
                    "tr",
                    null,
                    h("th", null, "Name"),
                    h("th", { className: "optional" }, "Owner"),
                    h("th", { className: "optional" }, "Modified"),
                    h("th", null, "Size"),
                    h("th", null, "Star"),
                  ),
                ),
                h(
                  "tbody",
                  null,
                  visible.map((f) =>
                    h(
                      "tr",
                      { key: f.id },
                      h(
                        "td",
                        null,
                        btn(
                          h("span", { className: "file-name" }, f.name),
                          () => open(f.id),
                          null,
                          "",
                          {
                            "aria-label": "Open " + f.name,
                            style: {
                              padding: "3px",
                              justifyContent: "flex-start",
                            },
                          },
                        ),
                      ),
                      h("td", { className: "optional" }, f.owner),
                      h("td", { className: "optional muted" }, f.date),
                      h("td", { className: "muted" }, f.size),
                      h(
                        "td",
                        null,
                        btn(
                          null,
                          () => patch(f.id, { star: !f.star }),
                          "star",
                          "",
                          {
                            "aria-label":
                              (f.star ? "Unstar " : "Star ") + f.name,
                            "aria-pressed": !!f.star,
                            style: {
                              color: f.star ? "var(--gold)" : "var(--quiet)",
                            },
                          },
                        ),
                      ),
                    ),
                  ),
                ),
              )
            : h(
                "div",
                { className: "file-grid" },
                visible.map((f) =>
                  h(
                    "div",
                    { key: f.id, className: "file-tile" },
                    btn(
                      h("div", { className: "file-cover" }, fileIcon(f)),
                      () => open(f.id),
                      null,
                      "",
                      {
                        "aria-label": "Open " + f.name,
                        style: { display: "block", width: "100%", padding: 0 },
                      },
                    ),
                    btn(f.name, () => open(f.id), null, "file-name", {
                      style: { padding: 0 },
                    }),
                    h(
                      "div",
                      { className: "flex spread" },
                      h("span", { className: "tiny muted" }, f.size),
                      btn(
                        null,
                        () => patch(f.id, { star: !f.star }),
                        "star",
                        "",
                        {
                          "aria-label": "Star " + f.name,
                          "aria-pressed": !!f.star,
                          style: {
                            color: f.star ? "var(--gold)" : "var(--quiet)",
                          },
                        },
                      ),
                    ),
                  ),
                ),
              ),
      ),
    ),
  );
}
