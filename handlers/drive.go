package handlers

import (
	"context"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"io"
	"strconv"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/workspace"
	"thura/schema"
)

func ListDrive(ctx context.Context, r *router.Request[router.None]) (schema.DriveListing, error) {
	w := r.Param("workspaceId")
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.DriveListing{}, err
	}
	queries := q.New(db.From(ctx))
	folders, err := queries.ListDriveFolders(ctx, w)
	if err != nil {
		return schema.DriveListing{}, err
	}
	files, err := queries.ListDriveFiles(ctx, w)
	out := schema.DriveListing{Folders: []schema.DriveFolder{}, Files: []schema.DriveFile{}}
	for _, f := range folders {
		out.Folders = append(out.Folders, drive.Folder(f))
	}
	for _, f := range files {
		out.Files = append(out.Files, drive.File(f))
	}
	return out, err
}
func CreateDriveFolder(ctx context.Context, r *router.Request[schema.FolderInput]) (schema.DriveFolder, error) {
	w := r.Param("workspaceId")
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.DriveFolder{}, err
	}
	queries := q.New(db.From(ctx))
	if err := drive.CheckFolder(ctx, queries, w, r.Body.ParentID); err != nil {
		return schema.DriveFolder{}, err
	}
	name, err := drive.Name(r.Body.Name)
	if err != nil {
		return schema.DriveFolder{}, err
	}
	f, err := queries.CreateDriveFolder(ctx, q.CreateDriveFolderParams{WorkspaceID: w, ParentID: r.Body.ParentID, Name: name})
	return drive.Folder(f), err
}
func BeginDriveUpload(ctx context.Context, r *router.Request[schema.UploadInput]) (schema.UploadState, error) {
	return drive.Begin(ctx, r.Param("workspaceId"), r.Body)
}
func GetDriveUpload(ctx context.Context, r *router.Request[router.None]) (schema.UploadState, error) {
	u, err := drive.Upload(ctx, r.Param("workspaceId"), r.Param("id"))
	if err != nil {
		return schema.UploadState{}, err
	}
	chunks, err := q.New(db.From(ctx)).ListUploadChunks(ctx, u.ID)
	out := schema.UploadState{Session: drive.Session(u), Chunks: []int{}}
	for _, c := range chunks {
		out.Chunks = append(out.Chunks, int(c.Number))
	}
	return out, err
}
func PutDriveChunk(ctx context.Context, r *router.Request[router.File]) (router.None, error) {
	n, err := strconv.Atoi(r.Param("number"))
	if err != nil {
		return router.None{}, router.Errorf(422, "invalid chunk number")
	}
	b, err := io.ReadAll(r.Body.Body)
	if err != nil {
		return router.None{}, err
	}
	return router.None{}, drive.PutChunk(ctx, r.Param("workspaceId"), r.Param("id"), n, b)
}
func FinishDriveUpload(ctx context.Context, r *router.Request[router.None]) (schema.DriveFile, error) {
	return drive.Finish(ctx, r.Param("workspaceId"), r.Param("id"))
}
func UpdateDriveFile(ctx context.Context, r *router.Request[schema.DriveFlags]) (schema.DriveFile, error) {
	w, id := r.Param("workspaceId"), r.Param("id")
	f, err := drive.Get(ctx, w, id)
	if err != nil {
		return schema.DriveFile{}, err
	}
	queries := q.New(db.From(ctx))
	if err = drive.CheckFolder(ctx, queries, w, r.Body.FolderID); err != nil {
		return schema.DriveFile{}, err
	}
	name := f.Name
	if r.Body.Name != nil {
		name, err = drive.Name(*r.Body.Name)
		if err != nil {
			return schema.DriveFile{}, err
		}
	}
	f, err = queries.UpdateDriveFlags(ctx, q.UpdateDriveFlagsParams{WorkspaceID: w, ID: id, Name: name, FolderID: r.Body.FolderID, Trashed: r.Body.Trashed})
	return drive.File(f), err
}
func DownloadDriveFile(ctx context.Context, r *router.Request[router.None]) (schema.FileContent, error) {
	f, err := drive.Get(ctx, r.Param("workspaceId"), r.Param("id"))
	if err != nil {
		return schema.FileContent{}, err
	}
	n := 0
	if r.Query("version") != "" {
		n, err = strconv.Atoi(r.Query("version"))
		if err != nil || n < 1 {
			return schema.FileContent{}, router.Errorf(422, "invalid version")
		}
	}
	return drive.Content(ctx, f, n)
}
func ListDriveVersions(ctx context.Context, r *router.Request[router.None]) (schema.VersionList, error) {
	f, err := drive.Get(ctx, r.Param("workspaceId"), r.Param("id"))
	if err != nil {
		return schema.VersionList{}, err
	}
	rows, err := q.New(db.From(ctx)).ListFileVersions(ctx, f.ID)
	out := schema.VersionList{Items: []schema.VersionView{}}
	for _, v := range rows {
		out.Items = append(out.Items, schema.VersionView{Number: int(v.Number), Checksum: v.Checksum, Size: int(v.Size), CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt})
	}
	return out, err
}
func CreateDriveShare(ctx context.Context, r *router.Request[schema.ShareInput]) (schema.ShareCreated, error) {
	return drive.Grant(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
}
func ListDriveShares(ctx context.Context, r *router.Request[router.None]) (schema.ShareList, error) {
	f, err := drive.Get(ctx, r.Param("workspaceId"), r.Param("id"))
	if err != nil {
		return schema.ShareList{}, err
	}
	rows, err := q.New(db.From(ctx)).ListShareGrants(ctx, f.ID)
	out := schema.ShareList{Items: []schema.ShareView{}}
	for _, g := range rows {
		out.Items = append(out.Items, drive.Share(g))
	}
	return out, err
}
func RevokeDriveShare(ctx context.Context, r *router.Request[router.None]) (router.None, error) {
	f, err := drive.Get(ctx, r.Param("workspaceId"), r.Param("id"))
	if err != nil {
		return router.None{}, err
	}
	if !workspace.ValidID(r.Param("shareId")) {
		return router.None{}, router.Errorf(404, "share not found")
	}
	n, err := q.New(db.From(ctx)).RevokeShareGrant(ctx, q.RevokeShareGrantParams{ID: r.Param("shareId"), FileID: f.ID})
	if err == nil && n == 0 {
		err = router.Errorf(404, "share not found")
	}
	return router.None{}, err
}
func OpenDriveShare(ctx context.Context, r *router.Request[schema.OpenShareInput]) (schema.FileContent, error) {
	return drive.Open(ctx, r.Body.Token)
}
