package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/encoding"
)

// ReplicationTrackedDetails captures the replication state shared across processes,
// including the active/passive folder selection and whether replication has failed.
type ReplicationTrackedDetails struct {
	FailedToReplicate bool
	// If true, folder as specified in storesBaseFolders[0] will be the active folder,
	// otherwise the 2nd folder, as specified in storesBaseFolders[1].
	ActiveFolderToggler bool

	// If true, Transactions should log commit changes so they can be used for "fast forward" functionality
	// of ReinstateFailedDrives functionality.
	LogCommitChanges bool
}

func (a ReplicationTrackedDetails) isEqual(b ReplicationTrackedDetails) bool {
	return a.ActiveFolderToggler == b.ActiveFolderToggler && a.FailedToReplicate == b.FailedToReplicate
}

type replicationTracker struct {
	ReplicationTrackedDetails
	// Array so we can use in replication across two folders, if in replication mode.
	storesBaseFolders []string
	replicate         bool
	l2Cache           sop.L2Cache
	tid               sop.UUID
}

const (
	replicationStatusFilename         = "replstat.txt"
	replicationStatusCacheTTLDuration = 5 * time.Minute
	commitChangesLogFolder            = "commitlogs"
)

func (r *replicationTracker) getReplicationStatusCacheKey() string {
	if len(r.storesBaseFolders) > 0 {
		return fmt.Sprintf("Rreplstat:%s", r.storesBaseFolders[0])
	}
	return "Rreplstat"
}

var GlobalReplicationDetails *ReplicationTrackedDetails
var globalReplicationDetailsLocker sync.RWMutex = sync.RWMutex{}

var recoveryStateCleanupPathMu sync.RWMutex
var recoveryStateCleanupPath string
var recoveryStateConfigUpdaterMu sync.RWMutex
var recoveryStateConfigUpdater func(path string) error

// SetRecoveryStateCleanupPath configures a recovery-state directory that should be
// removed from the passive side during failover only when it resides on the same
// underlying drive as the passive folder. This allows a user-selected recovery
// location on a dedicated drive to remain untouched.
func SetRecoveryStateCleanupPath(path string) {
	recoveryStateCleanupPathMu.Lock()
	defer recoveryStateCleanupPathMu.Unlock()
	recoveryStateCleanupPath = strings.TrimSpace(path)
}

// SetRecoveryStateConfigUpdater registers a callback used to persist the updated
// recovery-state path into configuration after reinstate rewrites it to the
// passive-drive location.
func SetRecoveryStateConfigUpdater(updater func(path string) error) {
	recoveryStateConfigUpdaterMu.Lock()
	defer recoveryStateConfigUpdaterMu.Unlock()
	recoveryStateConfigUpdater = updater
}

func getRecoveryStateCleanupPath() string {
	recoveryStateCleanupPathMu.RLock()
	defer recoveryStateCleanupPathMu.RUnlock()
	return recoveryStateCleanupPath
}

func persistRecoveryStatePath(path string) error {
	recoveryStateConfigUpdaterMu.RLock()
	updater := recoveryStateConfigUpdater
	recoveryStateConfigUpdaterMu.RUnlock()
	if updater == nil {
		return nil
	}
	return updater(strings.TrimSpace(path))
}

// NewReplicationTracker instantiates a tracker for active/passive folders and replication status.
// It reads persisted status, initializes global in-memory state, and optionally synchronizes with L2 cache.
func NewReplicationTracker(ctx context.Context, storesBaseFolders []string, replicate bool, l2Cache sop.L2Cache) (*replicationTracker, error) {
	if l2Cache == nil {
		return nil, fmt.Errorf("l2Cache can't be nil")
	}
	log.Debug(fmt.Sprintf("storesBaseFolders: %v", storesBaseFolders))

	isFirstFolderActive := true
	rt := replicationTracker{
		storesBaseFolders: storesBaseFolders,
		replicate:         replicate,
		l2Cache:           l2Cache,
	}
	rt.ActiveFolderToggler = isFirstFolderActive
	if replicate {
		globalReplicationDetailsLocker.Lock()
		if err := rt.syncWithL2Cache(ctx, false); err != nil {
			log.Warn(fmt.Sprintf("error while updating global replication status & L2 cache, details: %v", err))
		}
		globalReplicationDetailsLocker.Unlock()
		// Minimize reading the replication "status" if we have read it and is tracking it globally.
		globalReplicationDetailsLocker.RLock()
		if GlobalReplicationDetails != nil {
			rt.ReplicationTrackedDetails = *GlobalReplicationDetails
			globalReplicationDetailsLocker.RUnlock()
		} else {
			globalReplicationDetailsLocker.RUnlock()
			if err := rt.readStatusFromHomeFolder(ctx); err != nil {
				return nil, fmt.Errorf("failed reading replication status (%sº file, details: %v", replicationStatusFilename, err)
			}
			globalReplicationDetailsLocker.Lock()
			copy := rt.ReplicationTrackedDetails
			GlobalReplicationDetails = &copy
			// Sync l2 cache.
			if err := rt.syncWithL2Cache(ctx, true); err != nil {
				log.Warn(fmt.Sprintf("error while updating global replication status & L2 cache, details: %v", err))
			}

			globalReplicationDetailsLocker.Unlock()
		}
	}
	return &rt, nil
}

// SetTransactionID sets the transaction id for associating replication-related logs.
func (r *replicationTracker) SetTransactionID(tid sop.UUID) {
	r.tid = tid
}

// HandleReplicationRelatedError evaluates an I/O error and triggers failover when appropriate.
// Called by transaction code paths that detect storage I/O errors during commit/rollback.
func (r *replicationTracker) HandleReplicationRelatedError(ctx context.Context, ioError error, rollbackError error, rollbackSucceeded bool) {
	if !r.replicate {
		return
	}
	rootErr := errors.Unwrap(ioError)
	err1, ok1 := rootErr.(sop.Error)
	err2, ok2 := ioError.(sop.Error)
	if ok2 {
		err1 = err2
	}
	if ok1 || ok2 {
		log.Warn(fmt.Sprintf("a replication related error detected (rollback: %v), details: %v", rollbackSucceeded, err1))

		failoverError := err1.Code == sop.RestoreRegistryFileSectorFailure || err2.Code == sop.RestoreRegistryFileSectorFailure

		// Generally, no need to failover if rollback succeeded. It means that the IO error itself is temporary.
		// BUT if error is known to need failover then it should get failover.
		if !failoverError && rollbackSucceeded {
			return
		}
		if err1.Code >= sop.FailoverQualifiedError || err2.Code >= sop.FailoverQualifiedError {
			if sop.ShouldTriggerFailover(ioError, "", "") {
				log.Warn(fmt.Sprintf("Replication I/O error detected (code=%v). Triggering failover.", err1.Code))
				if err := r.failover(ctx); err != nil {
					log.Warn(fmt.Sprintf("replication failover failed, details: %v", err))
				}
			} else {
				log.Warn(fmt.Sprintf("Replication I/O error detected (code=%v). Failover threshold not reached yet.", err1.Code))
			}
		}
	}
}

func (r *replicationTracker) handleFailedToReplicate(ctx context.Context) {
	if !r.replicate || r.FailedToReplicate {
		return
	}

	globalReplicationDetailsLocker.Lock()
	if err := r.syncWithL2Cache(ctx, false); err != nil {
		log.Warn(fmt.Sprintf("error while updating global replication status & L2 cache, details: %v", err))
	}
	globalReplicationDetailsLocker.Unlock()

	// L2 cache "knows" of failure, just return.
	globalReplicationDetailsLocker.RLock()
	if GlobalReplicationDetails.FailedToReplicate {
		globalReplicationDetailsLocker.RUnlock()
		r.FailedToReplicate = true
		return
	}
	globalReplicationDetailsLocker.RUnlock()

	globalReplicationDetailsLocker.Lock()

	if r.FailedToReplicate {
		globalReplicationDetailsLocker.Unlock()
		return
	}

	r.FailedToReplicate = true
	GlobalReplicationDetails.FailedToReplicate = true
	if err := r.writeReplicationStatus(ctx, r.formatActiveFolderEntity(replicationStatusFilename)); err != nil {
		log.Warn(fmt.Sprintf("handleFailedToReplicate writeReplicationStatus failed, details: %v", err))
	}

	// Sync l2 cache.
	if err := r.syncWithL2Cache(ctx, true); err != nil {
		log.Warn(fmt.Sprintf("error while updating global replication status & L2 cache, details: %v", err))
	}

	globalReplicationDetailsLocker.Unlock()
}

func (r *replicationTracker) failover(ctx context.Context) error {
	globalReplicationDetailsLocker.RLock()
	if GlobalReplicationDetails.ActiveFolderToggler == !r.ActiveFolderToggler ||
		r.FailedToReplicate {
		globalReplicationDetailsLocker.RUnlock()
		// Do nothing if global tracker already knows that a failover already occurred.
		return nil
	}
	globalReplicationDetailsLocker.RUnlock()

	globalReplicationDetailsLocker.Lock()
	if err := r.syncWithL2Cache(ctx, false); err != nil {
		log.Warn(fmt.Sprintf("error while updating global replication status & L2 cache, details: %v", err))
	}
	globalReplicationDetailsLocker.Unlock()

	globalReplicationDetailsLocker.RLock()
	if GlobalReplicationDetails.ActiveFolderToggler == !r.ActiveFolderToggler {
		globalReplicationDetailsLocker.RUnlock()
		// Do nothing if global tracker already knows that a failover already occurred.
		return nil
	}
	globalReplicationDetailsLocker.RUnlock()

	globalReplicationDetailsLocker.Lock()
	if GlobalReplicationDetails.ActiveFolderToggler == !r.ActiveFolderToggler {
		globalReplicationDetailsLocker.Unlock()
		// Do nothing if global tracker already knows that a failover already occurred.
		return nil
	}

	// Set to failed to replicate because when we flip passive to active, then yes, we should not
	// replicate on the previously active drive because it failed.
	r.FailedToReplicate = true

	if err := r.writeReplicationStatus(ctx, r.formatPassiveFolderEntity(replicationStatusFilename)); err != nil {
		globalReplicationDetailsLocker.Unlock()
		return err
	}

	// Switch the passive into active & vice versa.
	r.ActiveFolderToggler = !r.ActiveFolderToggler
	copy := r.ReplicationTrackedDetails
	GlobalReplicationDetails = &copy

	// Sync l2 cache.
	if err := r.syncWithL2Cache(ctx, true); err != nil {
		log.Warn(fmt.Sprintf("error while updating global replication status & L2 cache, details: %v", err))
	}

	globalReplicationDetailsLocker.Unlock()

	if err := cleanupRecoveryStateOnFailover(r.getPassiveBaseFolder(), r.getActiveBaseFolder()); err != nil {
		log.Warn(fmt.Sprintf("cleanup recovery state after failover failed, details: %v", err))
	}

	log.Info(fmt.Sprintf("failover event occurred, newly active folder is, %s", r.getActiveBaseFolder()))
	return nil
}

func cleanupRecoveryStateOnFailover(passiveBaseFolder, activeBaseFolder string) error {
	cleanupPath := getRecoveryStateCleanupPath()
	if strings.TrimSpace(cleanupPath) == "" {
		return nil
	}
	if strings.TrimSpace(passiveBaseFolder) == "" || strings.TrimSpace(activeBaseFolder) == "" {
		return nil
	}
	if filepath.Clean(cleanupPath) == filepath.Clean(passiveBaseFolder) || filepath.Clean(cleanupPath) == filepath.Clean(activeBaseFolder) {
		return nil
	}
	if !sameDrivePath(cleanupPath, passiveBaseFolder) {
		return nil
	}
	return os.RemoveAll(cleanupPath)
}

func ensureRecoveryStatePathForPassiveDrive(passiveBaseFolder string) error {
	configuredPath := getRecoveryStateCleanupPath()
	if strings.TrimSpace(configuredPath) == "" || strings.TrimSpace(passiveBaseFolder) == "" {
		return nil
	}
	if !sameDrivePath(configuredPath, passiveBaseFolder) {
		return nil
	}
	derivedPath := deriveRecoveryStatePath(configuredPath, passiveBaseFolder)
	if strings.TrimSpace(derivedPath) == "" {
		return nil
	}
	if err := os.MkdirAll(derivedPath, 0o755); err != nil {
		return err
	}
	SetRecoveryStateCleanupPath(derivedPath)
	return persistRecoveryStatePath(derivedPath)
}

func deriveRecoveryStatePath(configuredPath, passiveBaseFolder string) string {
	cleanConfiguredPath := filepath.Clean(strings.TrimSpace(configuredPath))
	cleanPassiveBaseFolder := filepath.Clean(strings.TrimSpace(passiveBaseFolder))
	if cleanConfiguredPath == "" || cleanPassiveBaseFolder == "" {
		return ""
	}
	name := filepath.Base(cleanConfiguredPath)
	if name == "." || name == string(filepath.Separator) || name == ".." {
		return cleanPassiveBaseFolder
	}
	return filepath.Join(cleanPassiveBaseFolder, name)
}

func sameDrivePath(pathA, pathB string) bool {
	if strings.TrimSpace(pathA) == "" || strings.TrimSpace(pathB) == "" {
		return false
	}
	resolvedA, errA := filepath.Abs(pathA)
	resolvedB, errB := filepath.Abs(pathB)
	if errA == nil && errB == nil {
		if deviceA, errA := pathDeviceInfo(resolvedA); errA == nil {
			if deviceB, errB := pathDeviceInfo(resolvedB); errB == nil {
				return deviceA == deviceB
			}
		}
	}
	if volumeA := filepath.VolumeName(resolvedA); volumeA != "" && volumeA == filepath.VolumeName(resolvedB) {
		return true
	}
	return false
}

func pathDeviceInfo(path string) (uint64, error) {
	current := path
	for {
		info, err := os.Stat(current)
		if err == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				return uint64(stat.Dev), nil
			}
			return 0, fmt.Errorf("unsupported stat implementation")
		}
		if !os.IsNotExist(err) {
			return 0, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return 0, os.ErrNotExist
}

func (r *replicationTracker) logCommitChanges(ctx context.Context, tid sop.UUID, stores []sop.StoreInfo, newRootNodesHandles, addedNodesHandles, updatedNodesHandles, removedNodesHandles []sop.RegistryPayload[sop.Handle]) error {
	if !r.LogCommitChanges {
		return nil
	}
	fn := r.formatActiveFolderEntity(fmt.Sprintf("%s%c%s%s", commitChangesLogFolder, os.PathSeparator, tid.String(), logFileExtension))

	fio := NewFileIO()

	// Payload conversion has to happen here so the payload unpack and operation on it can be done on this file as well.
	payload, err := encoding.DefaultMarshaler.Marshal(sop.Tuple[[]sop.StoreInfo, [][]sop.RegistryPayload[sop.Handle]]{
		First: stores,
		Second: [][]sop.RegistryPayload[sop.Handle]{
			newRootNodesHandles, addedNodesHandles,
			updatedNodesHandles, removedNodesHandles,
		},
	})
	if err != nil {
		return err
	}

	return fio.WriteFile(ctx, fn, payload, permission)
}

func (r *replicationTracker) getActiveBaseFolder() string {
	if len(r.storesBaseFolders) == 0 {
		return ""
	}
	if r.ActiveFolderToggler {
		return r.storesBaseFolders[0]
	}
	if len(r.storesBaseFolders) == 1 {
		return r.storesBaseFolders[0]
	}
	return r.storesBaseFolders[1]
}
func (r *replicationTracker) getPassiveBaseFolder() string {
	if len(r.storesBaseFolders) == 0 {
		return ""
	}
	if r.ActiveFolderToggler {
		if len(r.storesBaseFolders) == 1 {
			return r.storesBaseFolders[0]
		}
		return r.storesBaseFolders[1]
	}
	return r.storesBaseFolders[0]
}

func (r *replicationTracker) formatActiveFolderEntity(entityName string) string {
	bf := r.getActiveBaseFolder()

	if strings.HasSuffix(bf, string(os.PathSeparator)) {
		return fmt.Sprintf("%s%s", bf, entityName)
	} else {
		return fmt.Sprintf("%s%c%s", bf, os.PathSeparator, entityName)
	}
}

func (r *replicationTracker) formatPassiveFolderEntity(entityName string) string {
	bf := r.getPassiveBaseFolder()

	if strings.HasSuffix(bf, string(os.PathSeparator)) {
		return fmt.Sprintf("%s%s", bf, entityName)
	} else {
		return fmt.Sprintf("%s%c%s", bf, os.PathSeparator, entityName)
	}
}

func (r *replicationTracker) readStatusFromHomeFolder(ctx context.Context) error {
	fio := NewFileIO()
	// Detect the active folder based on time stamp of the file.
	if !fio.Exists(ctx, r.formatActiveFolderEntity(replicationStatusFilename)) {
		if fio.Exists(ctx, r.formatPassiveFolderEntity(replicationStatusFilename)) {
			if err := r.readReplicationStatus(ctx, r.formatPassiveFolderEntity(replicationStatusFilename)); err == nil {
				// Switch passive to active if we are able to read the delta sync status file.
				r.ActiveFolderToggler = !r.ActiveFolderToggler
			}
		}
		// No Replication status file in both active & passive folders, we're good with default.
		return nil
	}
	stat, err := os.Stat(r.formatActiveFolderEntity(replicationStatusFilename))
	if err != nil {
		_, err = os.Stat(r.formatPassiveFolderEntity(replicationStatusFilename))
		if err != nil {
			return err
		}
		r.ActiveFolderToggler = !r.ActiveFolderToggler
	} else {
		stat2, err := os.Stat(r.formatPassiveFolderEntity(replicationStatusFilename))
		if err == nil {
			if stat2.ModTime().After(stat.ModTime()) {
				r.ActiveFolderToggler = !r.ActiveFolderToggler
			}
		}
	}

	return r.readReplicationStatus(ctx, r.formatActiveFolderEntity(replicationStatusFilename))
}

func (r *replicationTracker) writeReplicationStatus(ctx context.Context, filename string) error {
	fio := NewFileIO()
	ba, _ := encoding.DefaultMarshaler.Marshal(r.ReplicationTrackedDetails)
	if err := fio.WriteFile(ctx, filename, ba, permission); err != nil {
		return err
	}
	return nil
}

func (r *replicationTracker) readReplicationStatus(ctx context.Context, filename string) error {
	fio := NewFileIO()
	// Read the delta sync status.
	ba, err := fio.ReadFile(ctx, filename)
	if err != nil {
		return err
	}
	if err = encoding.DefaultMarshaler.Unmarshal(ba, &r.ReplicationTrackedDetails); err != nil {
		return err
	}
	return nil
}

// syncWithL2Cache synchronizes replication state with L2 cache.
// When pushValue is true, it pushes the current global state; otherwise it pulls the cached state.
func (r *replicationTracker) syncWithL2Cache(ctx context.Context, pushValue bool) error {

	var rtd ReplicationTrackedDetails
	key := r.getReplicationStatusCacheKey()
	// Update L2 cache of new value in global status.
	if pushValue {
		// When stable, perhaps we just issue a SetStruct here to sync L2 cache.
		if found, err := r.l2Cache.GetStructEx(ctx, key, &rtd, replicationStatusCacheTTLDuration); err != nil {
			return err
		} else if !found {
			if err := r.l2Cache.SetStruct(ctx, key, *GlobalReplicationDetails, replicationStatusCacheTTLDuration); err != nil {
				return err
			}
			return nil
		}
		// Found in L2 cache, sync it if needed.
		if rtd.isEqual(*GlobalReplicationDetails) {
			log.Debug("global replication details & l2 Cache copy is found to be in sync")
			return nil
		}
		if err := r.l2Cache.SetStruct(ctx, key, *GlobalReplicationDetails, replicationStatusCacheTTLDuration); err != nil {
			return err
		}
		log.Debug(fmt.Sprintf("l2 cache had been updated with global replication details value: %v", GlobalReplicationDetails))
		return nil
	}
	// pull or update global replication details with l2 cache copy.
	if found, err := r.l2Cache.GetStructEx(ctx, key, &rtd, replicationStatusCacheTTLDuration); err != nil {
		return err
	} else if !found {
		log.Debug("replication details not found in l2 cache")
		return nil
	}

	log.Debug(fmt.Sprintf("global replication details & this repl object are being updated w/ l2 cache copy: %v", rtd))

	// Just assign to global replication details the read rtd value from l2 cache.
	GlobalReplicationDetails = &rtd

	return nil
}

// TriggerFailover is a helper to manually trigger failover for testing purposes.
func TriggerFailover(ctx context.Context, storesBaseFolders []string, replicate bool, l2Cache sop.L2Cache) error {
	rt, err := NewReplicationTracker(ctx, storesBaseFolders, replicate, l2Cache)
	if err != nil {
		return err
	}
	return rt.failover(ctx)
}
