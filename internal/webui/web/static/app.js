document.addEventListener('alpine:init', () => {
  Alpine.data('app', () => ({

    // ── Page navigation ──────────────────────────────────────────────────────
    page: 'dashboard',

    // ── Sidebar nav state ────────────────────────────────────────────────────
    navTypesOpen:     true,
    navDatesOpen:     true,
    navTagsOpen:      false,
    navTypes:         [],
    navDates:         [],
    navTags:          [],
    navExpandedYears: {},
    navDuplicateCount: 0,

    // ── Active file-browser filters ──────────────────────────────────────────
    filterExt:        '',
    filterYear:       '',
    filterMonth:      '',
    filterTag:        '',
    filterDuplicates: false,

    // ── Dashboard data ───────────────────────────────────────────────────────
    stats:         null,
    recentHistory: [],

    // ── Files browser ────────────────────────────────────────────────────────
    files:        [],
    filesResult:  null,
    filesLoading: false,
    filesPage:    1,
    filesQuery:   '',
    filesSort:    'mod_time',
    filesOrder:   'desc',
    viewMode:     'list',

    // ── History log ──────────────────────────────────────────────────────────
    history:             [],
    historyResult:       null,
    historyLoading:      false,
    historyPage:         1,
    historyStatusFilter: '',

    // ── Media viewer ─────────────────────────────────────────────────────────
    viewerOpen:            false,
    viewerFile:            null,
    viewerIndex:           -1,
    viewerSidebarOpen:     false,
    viewerTab:             'info',
    viewerFileHistory:     [],
    viewerHistoryLoading:  false,
    viewerTextContent:     '',
    viewerTextLoading:     false,
    viewerFileTags:        [],
    viewerTagSearch:       '',
    viewerTagDropdownOpen: false,
    viewerTagSuggestions:  [],
    thumbErrors:           {},

    // ── Tags page ─────────────────────────────────────────────────────────────
    allTags:               [],  // full flat list
    tagsPageCategories:    [],  // categories with tags embedded
    tagsPageUncategorised: [],
    allTagsForMerge:       [],

    // Tag category modal
    catModalOpen: false,
    catModalData: { id: null, name: '', color: '#6b7280' },

    // Tag modal
    tagModalOpen: false,
    tagModalData: { id: null, name: '', categoryId: '', mergeIntoId: '' },

    // Confirm delete modal
    confirmDeleteModalOpen: false,
    confirmDeleteTarget:    null,
    confirmDeleteType:      '',  // 'category' | 'tag'

    // ── Duplicates page ───────────────────────────────────────────────────────
    dupGroups:          [],
    dupLoading:         false,
    dupDismissed:       {},   // { groupIndex: true }
    dupConfirmOpen:     false,
    dupConfirmTitle:    '',
    dupConfirmMessage:  '',
    dupConfirmPath:     '',
    dupConfirmAction:   '',
    dupConfirmDanger:   true,
    _dupConfirmFn:      null,

    // ── Toast notification ────────────────────────────────────────────────────
    toastMsg:     '',
    toastVisible: false,

    // ── Computed ─────────────────────────────────────────────────────────────
    get chips() {
      const c = [];
      if (this.filterExt)
        c.push({ label: '.' + this.filterExt,                      key: 'ext' });
      if (this.filterYear && !this.filterMonth)
        c.push({ label: this.filterYear,                           key: 'year' });
      if (this.filterMonth)
        c.push({ label: this.filterYear + '-' + this.filterMonth,  key: 'month' });
      if (this.filterTag)
        c.push({ label: this.filterTag,                            key: 'tag' });
      if (this.filterDuplicates)
        c.push({ label: 'duplicates only',                         key: 'duplicates' });
      return c;
    },

    get visibleGroups() {
      return this.dupGroups.filter((_, i) => !this.dupDismissed[i]);
    },

    get identicalCount() {
      return this.visibleGroups.filter(g =>
        g.duplicates && g.duplicates.some(d => this.dupIsIdentical(g, d))
      ).length;
    },

    get viewerType() {
      if (!this.viewerFile) return 'other';
      return this.viewerTypeForExt(this.viewerFile.extension || '');
    },

    // ── Lifecycle ────────────────────────────────────────────────────────────
    init() {
      this.loadStats();
      this.loadRecentHistory();
      this.loadNavData();
      this.loadAllTags();

      this.$watch('page', p => {
        if (p === 'files')      this.loadFiles();
        if (p === 'history')    this.loadHistory();
        if (p === 'tags')       this.loadTagsPage();
        if (p === 'duplicates') this.loadDuplicates();
      });
      this.$watch('filesPage',  () => this.loadFiles());
      this.$watch('filesQuery', () => { this.filesPage = 1; this.loadFiles(); });
      this.$watch('filesSort',  () => { this.filesPage = 1; this.loadFiles(); });
      this.$watch('filesOrder', () => { this.filesPage = 1; this.loadFiles(); });
      this.$watch('historyPage', () => this.loadHistory());
      this.$watch('historyStatusFilter', () => { this.historyPage = 1; this.loadHistory(); });
    },

    // ── Data loaders ─────────────────────────────────────────────────────────
    async loadStats() {
      try {
        const res = await fetch('/api/stats');
        this.stats = await res.json();
      } catch(e) { console.error('stats', e); }
    },

    async loadRecentHistory() {
      try {
        const res = await fetch('/api/history/recent');
        this.recentHistory = await res.json() ?? [];
      } catch(e) { console.error('recent history', e); }
    },

    async loadNavData() {
      try {
        const [types, dates, tags] = await Promise.all([
          fetch('/api/nav/types').then(r => r.json()),
          fetch('/api/nav/dates').then(r => r.json()),
          fetch('/api/nav/tags').then(r => r.json()),
        ]);
        this.navTypes = types ?? [];
        this.navDates = dates ?? [];
        this.navTags  = tags  ?? [];

        const dup = await fetch('/api/files?duplicates_only=true&per_page=1').then(r => r.json());
        this.navDuplicateCount = dup.total ?? 0;
      } catch(e) { console.error('nav data', e); }
    },

    async loadFiles() {
      this.filesLoading = true;
      try {
        const p = new URLSearchParams({
          page:     this.filesPage,
          per_page: 50,
          sort:     this.filesSort,
          order:    this.filesOrder,
        });
        if (this.filesQuery)       p.set('q',              this.filesQuery);
        if (this.filterExt)        p.set('ext',            this.filterExt);
        if (this.filterYear)       p.set('year',           this.filterYear);
        if (this.filterMonth)      p.set('month',          this.filterMonth);
        if (this.filterTag)        p.set('tag',            this.filterTag);
        if (this.filterDuplicates) p.set('duplicates_only','true');

        const res = await fetch('/api/files?' + p);
        this.filesResult = await res.json();
        const raw = this.filesResult.files ?? [];

        // Eagerly load tags for each file in parallel (batched).
        this.files = await Promise.all(raw.map(async f => {
          try {
            const tr = await fetch(`/api/files/${f.id}/tags`);
            f.tags = await tr.json() ?? [];
          } catch { f.tags = []; }
          return f;
        }));
      } catch(e) {
        console.error('files', e);
      } finally {
        this.filesLoading = false;
      }
    },

    async loadHistory() {
      this.historyLoading = true;
      try {
        const p = new URLSearchParams({ page: this.historyPage, per_page: 50 });
        if (this.historyStatusFilter) p.set('status', this.historyStatusFilter);
        const res = await fetch('/api/history?' + p);
        this.historyResult = await res.json();
        this.history = this.historyResult.entries ?? [];
      } catch(e) {
        console.error('history', e);
      } finally {
        this.historyLoading = false;
      }
    },

    // ── Tags page loaders ─────────────────────────────────────────────────────
    async loadTagsPage() {
      try {
        const [cats, tags] = await Promise.all([
          fetch('/api/tag-categories').then(r => r.json()),
          fetch('/api/tags').then(r => r.json()),
        ]);
        this.allTags = tags ?? [];

        // Group tags under categories
        const catMap = {};
        for (const cat of cats ?? []) {
          catMap[cat.id] = { ...cat, tags: [] };
        }
        this.tagsPageUncategorised = [];
        for (const tag of this.allTags) {
          if (tag.category_id && catMap[tag.category_id]) {
            catMap[tag.category_id].tags.push(tag);
          } else {
            this.tagsPageUncategorised.push(tag);
          }
        }
        this.tagsPageCategories = Object.values(catMap);
      } catch(e) { console.error('tags page', e); }
    },

    async loadAllTags() {
      try {
        const res = await fetch('/api/tags');
        this.allTags = await res.json() ?? [];
      } catch(e) { console.error('load all tags', e); }
    },

    // ── Navigation helpers ───────────────────────────────────────────────────
    setFilter(key, val, val2) {
      this.filterExt        = '';
      this.filterYear       = '';
      this.filterMonth      = '';
      this.filterTag        = '';
      this.filterDuplicates = false;
      this.filesQuery       = '';
      this.filesPage        = 1;

      switch (key) {
        case 'ext':        this.filterExt        = val;  break;
        case 'year':       this.filterYear       = val;  break;
        case 'month':      this.filterYear = val; this.filterMonth = val2; break;
        case 'tag':        this.filterTag        = val;  break;
        case 'duplicates': this.filterDuplicates = true; break;
      }
      this.page = 'files';
      this.loadFiles();
    },

    clearChip(key) {
      switch (key) {
        case 'ext':        this.filterExt        = ''; break;
        case 'year':       this.filterYear       = ''; this.filterMonth = ''; break;
        case 'month':      this.filterMonth      = ''; break;
        case 'tag':        this.filterTag        = ''; break;
        case 'duplicates': this.filterDuplicates = false; break;
      }
      this.filesPage = 1;
      this.loadFiles();
    },

    clearAllFilters() {
      this.filterExt = ''; this.filterYear = ''; this.filterMonth = '';
      this.filterTag = ''; this.filterDuplicates = false;
      this.filesQuery = ''; this.filesPage = 1;
      this.loadFiles();
    },

    // ── Tags page actions ────────────────────────────────────────────────────
    openCatModal(cat) {
      if (cat) {
        this.catModalData = { id: cat.id, name: cat.name, color: cat.color };
      } else {
        this.catModalData = { id: null, name: '', color: '#6b7280' };
      }
      this.catModalOpen = true;
    },

    async saveCategoryModal() {
      const { id, name, color } = this.catModalData;
      if (!name) return;
      try {
        if (id) {
          await fetch(`/api/tag-categories/${id}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, color }),
          });
        } else {
          await fetch('/api/tag-categories', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, color }),
          });
        }
        this.catModalOpen = false;
        await this.loadTagsPage();
        await this.loadNavData();
        this.showToast('Category saved');
      } catch(e) { console.error(e); }
    },

    openTagModal(categoryId, tag) {
      if (tag) {
        this.tagModalData = {
          id: tag.id,
          name: tag.name,
          categoryId: tag.category_id ?? '',
          mergeIntoId: '',
        };
        // Build merge list (exclude self)
        this.allTagsForMerge = this.allTags.filter(t => t.id !== tag.id);
      } else {
        this.tagModalData = { id: null, name: '', categoryId: categoryId ?? '', mergeIntoId: '' };
        this.allTagsForMerge = [];
      }
      this.tagModalOpen = true;
    },

    async saveTagModal() {
      const { id, name, categoryId, mergeIntoId } = this.tagModalData;
      if (!name && !mergeIntoId) return;
      try {
        if (mergeIntoId) {
          await fetch(`/api/tags/${id}/merge`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ into_id: parseInt(mergeIntoId, 10) }),
          });
          this.showToast('Tags merged');
        } else if (id) {
          const body = { name };
          if (categoryId !== '') {
            body.category_id = categoryId ? parseInt(categoryId, 10) : null;
            body.change_category = true;
          }
          await fetch(`/api/tags/${id}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
          this.showToast('Tag saved');
        } else {
          await fetch('/api/tags', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              name,
              category_id: categoryId ? parseInt(categoryId, 10) : 0,
            }),
          });
          this.showToast('Tag created');
        }
        this.tagModalOpen = false;
        await this.loadTagsPage();
        await this.loadNavData();
      } catch(e) { console.error(e); }
    },

    confirmDeleteCategory(cat) {
      this.confirmDeleteTarget = cat;
      this.confirmDeleteType   = 'category';
      this.confirmDeleteModalOpen = true;
    },

    confirmDeleteTag(tag) {
      this.confirmDeleteTarget = tag;
      this.confirmDeleteType   = 'tag';
      this.confirmDeleteModalOpen = true;
    },

    async executeDelete() {
      try {
        if (this.confirmDeleteType === 'category') {
          await fetch(`/api/tag-categories/${this.confirmDeleteTarget.id}`, { method: 'DELETE' });
          this.showToast('Category deleted');
        } else {
          await fetch(`/api/tags/${this.confirmDeleteTarget.id}`, { method: 'DELETE' });
          this.showToast('Tag deleted');
        }
        this.confirmDeleteModalOpen = false;
        await this.loadTagsPage();
        await this.loadNavData();
      } catch(e) { console.error(e); }
    },

    // ── Media viewer ─────────────────────────────────────────────────────────
    async openViewer(file, index) {
      this.viewerFile  = file;
      this.viewerIndex = index;
      this.viewerOpen  = true;
      this.viewerTab   = 'info';
      this.viewerTextContent    = '';
      this.viewerFileHistory    = [];
      this.viewerFileTags       = file.tags ?? [];
      this.viewerTagSearch      = '';
      this.viewerTagDropdownOpen = false;
      document.body.style.overflow = 'hidden';

      if (this.viewerType === 'text') this.loadViewerText(file);
    },

    closeViewer() {
      this.viewerOpen  = false;
      this.viewerFile  = null;
      this.viewerIndex = -1;
      this.viewerFileHistory = [];
      this.viewerFileTags    = [];
      this.viewerTextContent = '';
      document.body.style.overflow = '';
      // Refresh file list in case tags changed
      if (this.page === 'files') this.loadFiles();
    },

    prevFile() {
      if (this.viewerIndex > 0) {
        const i = this.viewerIndex - 1;
        this.openViewer(this.files[i], i);
      }
    },

    nextFile() {
      if (this.viewerIndex < this.files.length - 1) {
        const i = this.viewerIndex + 1;
        this.openViewer(this.files[i], i);
      }
    },

    async loadViewerText(file) {
      this.viewerTextLoading = true;
      try {
        const res = await fetch(`/api/files/${file.id}/content`);
        const text = await res.text();
        this.viewerTextContent = text
          .replace(/&/g,'&amp;')
          .replace(/</g,'&lt;')
          .replace(/>/g,'&gt;');
      } catch(e) {
        this.viewerTextContent = 'Error loading file content.';
      } finally {
        this.viewerTextLoading = false;
      }
    },

    async loadViewerHistory() {
      if (!this.viewerFile) return;
      this.viewerHistoryLoading = true;
      this.viewerTab = 'history';
      try {
        const res = await fetch(`/api/files/${this.viewerFile.id}/history`);
        this.viewerFileHistory = await res.json() ?? [];
      } catch(e) {
        this.viewerFileHistory = [];
      } finally {
        this.viewerHistoryLoading = false;
      }
    },

    // ── Viewer tag editor ─────────────────────────────────────────────────────
    filterViewerTags() {
      const q = this.viewerTagSearch.toLowerCase();
      if (!q) { this.viewerTagSuggestions = []; return; }
      const currentIds = new Set(this.viewerFileTags.map(t => t.id));
      this.viewerTagSuggestions = this.allTags
        .filter(t => !currentIds.has(t.id) && t.name.toLowerCase().includes(q))
        .slice(0, 8);
    },

    async addTagFromViewer(tag) {
      if (!this.viewerFile) return;
      const newIds = [...this.viewerFileTags.map(t => t.id), tag.id];
      await this._saveFileTags(newIds);
      this.viewerTagSearch = '';
      this.viewerTagDropdownOpen = false;
    },

    async removeTagFromViewer(tagId) {
      if (!this.viewerFile) return;
      const newIds = this.viewerFileTags.filter(t => t.id !== tagId).map(t => t.id);
      await this._saveFileTags(newIds);
    },

    async _saveFileTags(tagIds) {
      try {
        const res = await fetch(`/api/files/${this.viewerFile.id}/tags`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ tag_ids: tagIds }),
        });
        this.viewerFileTags = await res.json() ?? [];
        // Update the in-memory file object so the list view stays fresh.
        if (this.viewerFile) this.viewerFile.tags = this.viewerFileTags;
      } catch(e) { console.error('save file tags', e); }
    },

    thumbnailURL(id)  { return `/api/files/${id}/thumbnail`; },
    downloadURL(id)   { return `/api/files/${id}/content?download=true`; },
    contentURL(id)    { return `/api/files/${id}/content`; },

    isImageExt(ext) {
      return ['jpg','jpeg','png','gif','bmp','webp','svg','tiff','tif','heic','heif'].includes(ext);
    },

    viewerTypeForExt(ext) {
      const e = (ext || '').toLowerCase();
      if (['jpg','jpeg','png','gif','bmp','webp','svg','tiff','tif','heic','heif'].includes(e)) return 'image';
      if (['mp4','mov','avi','mkv','webm','m4v','wmv','flv','ogv'].includes(e)) return 'video';
      if (['mp3','wav','flac','aac','m4a','ogg','wma','opus'].includes(e)) return 'audio';
      if (['pdf'].includes(e)) return 'pdf';
      if (['txt','md','json','yaml','yml','csv','xml','log','js','ts','go','py',
           'sh','bash','zsh','c','cpp','h','java','rb','rs','toml','ini','conf','env'].includes(e)) return 'text';
      return 'other';
    },

    onThumbError(id) {
      this.thumbErrors = { ...this.thumbErrors, [id]: true };
    },

    // ── Duplicates page ───────────────────────────────────────────────────────
    async loadDuplicates() {
      this.dupLoading = true;
      this.dupDismissed = {};
      try {
        const res = await fetch('/api/duplicates');
        this.dupGroups = await res.json() ?? [];
      } catch(e) {
        console.error('duplicates', e);
        this.dupGroups = [];
      } finally {
        this.dupLoading = false;
      }
    },

    refreshDuplicates() {
      this.loadDuplicates();
    },

    dupIsIdentical(group, dup) {
      if (!group.primary || !dup) return false;
      return group.primary.checksum && dup.checksum &&
             group.primary.checksum === dup.checksum;
    },

    dismissGroup(index) {
      this.dupDismissed = { ...this.dupDismissed, [index]: true };
    },

    fileExt(filename) {
      if (!filename) return '';
      const i = filename.lastIndexOf('.');
      return i >= 0 ? filename.slice(i + 1).toLowerCase() : '';
    },

    confirmDeleteDup(dup, group) {
      this.dupConfirmTitle   = 'Delete duplicate file?';
      this.dupConfirmMessage = `This will remove the duplicate from the archive and its database record.`;
      this.dupConfirmPath    = dup.archive_path;
      this.dupConfirmAction  = 'Delete duplicate';
      this.dupConfirmDanger  = true;
      this._dupConfirmFn     = async () => {
        await this._doDeleteDup(dup.id);
        this._removeFromGroup(group, dup);
      };
      this.dupConfirmOpen    = true;
    },

    confirmPromote(dup, group) {
      this.dupConfirmTitle   = 'Promote duplicate to primary?';
      this.dupConfirmMessage = `The duplicate will be moved to the primary location. The existing primary will be removed.`;
      this.dupConfirmPath    = dup.archive_path;
      this.dupConfirmAction  = 'Promote';
      this.dupConfirmDanger  = false;
      this._dupConfirmFn     = async () => {
        await this._doPromote(dup.id, group.primary ? group.primary.id : null);
        await this.loadDuplicates();
      };
      this.dupConfirmOpen    = true;
    },

    confirmBulkDelete() {
      this.dupConfirmTitle   = 'Delete all identical duplicates?';
      this.dupConfirmMessage = `Files where the duplicate's checksum exactly matches the primary will be permanently deleted from disk and the database. This cannot be undone.`;
      this.dupConfirmPath    = '';
      this.dupConfirmAction  = 'Delete identical';
      this.dupConfirmDanger  = true;
      this._dupConfirmFn     = async () => {
        const res = await fetch('/api/duplicates/bulk-delete-identical?confirm=true', { method: 'POST' });
        if (!res.ok) {
          const j = await res.json().catch(() => ({}));
          this.showToast('Error: ' + (j.error || res.status));
          return;
        }
        const j = await res.json();
        this.showToast(`Deleted ${j.deleted ?? 0} duplicate file(s)`);
        await this.loadDuplicates();
        this.loadStats();
      };
      this.dupConfirmOpen    = true;
    },

    async executeDupAction() {
      this.dupConfirmOpen = false;
      if (this._dupConfirmFn) {
        await this._dupConfirmFn();
        this._dupConfirmFn = null;
      }
    },

    async _doDeleteDup(fileId) {
      const res = await fetch(`/api/files/${fileId}?confirm=true`, { method: 'DELETE' });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        this.showToast('Error: ' + (j.error || res.status));
        return false;
      }
      this.showToast('Duplicate deleted');
      this.loadStats();
      return true;
    },

    async _doPromote(dupId, primaryId) {
      const body = primaryId ? JSON.stringify({ primary_id: primaryId }) : '{}';
      const res = await fetch(`/api/duplicates/${dupId}/promote`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body,
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        this.showToast('Error: ' + (j.error || res.status));
        return false;
      }
      this.showToast('Promoted to primary');
      this.loadStats();
      return true;
    },

    _removeFromGroup(group, dup) {
      group.duplicates = group.duplicates.filter(d => d.id !== dup.id);
      if (group.duplicates.length === 0) {
        const idx = this.dupGroups.indexOf(group);
        if (idx >= 0) this.dupGroups.splice(idx, 1);
      }
    },

    // ── Toast ─────────────────────────────────────────────────────────────────
    showToast(msg) {
      this.toastMsg = msg;
      this.toastVisible = true;
      setTimeout(() => { this.toastVisible = false; }, 2500);
    },

    // ── Presentation helpers ─────────────────────────────────────────────────
    monthName(m) {
      const names = ['Jan','Feb','Mar','Apr','May','Jun',
                     'Jul','Aug','Sep','Oct','Nov','Dec'];
      return names[parseInt(m, 10) - 1] ?? m;
    },

    extColor(ext) {
      const map = {
        jpg:'#3b82f6', jpeg:'#3b82f6', png:'#3b82f6', gif:'#3b82f6',
        webp:'#3b82f6', svg:'#3b82f6', tiff:'#3b82f6', bmp:'#3b82f6',
        heic:'#3b82f6', heif:'#3b82f6', raw:'#3b82f6',
        mp4:'#8b5cf6', mov:'#8b5cf6', avi:'#8b5cf6', mkv:'#8b5cf6',
        webm:'#8b5cf6', m4v:'#8b5cf6', wmv:'#8b5cf6', flv:'#8b5cf6',
        mp3:'#10b981', wav:'#10b981', flac:'#10b981', aac:'#10b981',
        m4a:'#10b981', ogg:'#10b981', wma:'#10b981', opus:'#10b981',
        pdf:'#ef4444',
        doc:'#f59e0b', docx:'#f59e0b', xls:'#f59e0b', xlsx:'#f59e0b',
        ppt:'#f59e0b', pptx:'#f59e0b', odt:'#f59e0b', ods:'#f59e0b',
        txt:'#6b7280', md:'#6b7280',  json:'#6b7280', yaml:'#6b7280',
        yml:'#6b7280', csv:'#6b7280', xml:'#6b7280',  log:'#6b7280',
        js:'#6b7280',  ts:'#6b7280',  go:'#6b7280',   py:'#6b7280',
      };
      return map[ext] ?? '#9ca3af';
    },

    extLabel(ext) {
      return (ext || 'FILE').toUpperCase().substring(0, 5);
    },

    formatBytes(bytes) {
      if (!bytes || bytes === 0) return '0 B';
      const units = ['B','KB','MB','GB','TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(1024));
      return (bytes / Math.pow(1024, i)).toFixed(1) + '\u00a0' + units[i];
    },

    formatDate(iso) {
      if (!iso) return '';
      try {
        return new Date(iso).toLocaleDateString(undefined,
          { year:'numeric', month:'short', day:'numeric' });
      } catch { return iso; }
    },

    formatDateTime(iso) {
      if (!iso) return '';
      try {
        return new Date(iso).toLocaleString(undefined,
          { year:'numeric', month:'short', day:'numeric',
            hour:'2-digit', minute:'2-digit' });
      } catch { return iso; }
    },
  }));
});
