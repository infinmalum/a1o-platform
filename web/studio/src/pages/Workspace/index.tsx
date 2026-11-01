import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FileText, Image as ImageIcon, MessageCircle, Wrench } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { statusBadgeVariant } from '@/pages/Work/workApi';
import { formatBytes, workspaceApi, type WorkspaceFile, type WorkspaceRun } from './workspaceApi';

// Workspace (R4): the user's material base — run history across Chat and
// Work (one run fact source) plus the shared files. Read-only views over
// existing resources; uploads stay in Chat.
const Workspace: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [runs, setRuns] = useState<WorkspaceRun[]>([]);
  const [files, setFiles] = useState<WorkspaceFile[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    Promise.all([workspaceApi.listRuns(), workspaceApi.listFiles()])
      .then(([runData, fileData]) => {
        if (cancelled) return;
        setRuns(runData || []);
        setFiles(fileData || []);
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const runTitle = (run: WorkspaceRun) =>
    run.kind === 'work'
      ? run.task_title || t('workspace.untitledTask')
      : run.session_title || t('workspace.untitledChat');

  const openRun = (run: WorkspaceRun) => {
    if (run.kind === 'work' && run.task_id > 0) {
      navigate(`/work/${run.task_id}`);
    } else if (run.kind === 'chat' && run.session_id > 0) {
      navigate(`/chat?session=${run.session_id}`);
    }
  };

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-10'>
      <div className='mb-8'>
        <h1 className='text-2xl font-semibold tracking-tight'>{t('workspace.title')}</h1>
        <p className='text-muted-foreground mt-1 text-sm'>{t('workspace.tagline')}</p>
      </div>

      <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('workspace.runsSection')}</h2>
      {loading ? (
        <p className='text-muted-foreground text-sm'>{t('workspace.loading')}</p>
      ) : runs.length === 0 ? (
        <p className='text-muted-foreground mb-8 text-sm'>{t('workspace.emptyRuns')}</p>
      ) : (
        <div className='mb-8 overflow-hidden rounded-xl border'>
          <table className='w-full text-sm'>
            <thead>
              <tr className='border-b bg-muted/40 text-left text-xs text-muted-foreground'>
                <th className='px-4 py-2 font-medium'>{t('workspace.runName')}</th>
                <th className='px-4 py-2 font-medium'>{t('workspace.runKind')}</th>
                <th className='px-4 py-2 font-medium'>{t('workspace.runStatus')}</th>
                <th className='px-4 py-2 font-medium'>{t('workspace.runModel')}</th>
                <th className='px-4 py-2 font-medium'>{t('workspace.runTime')}</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr
                  key={run.id}
                  className='cursor-pointer border-b last:border-b-0 hover:bg-accent/40'
                  onClick={() => openRun(run)}
                >
                  <td className='max-w-[220px] truncate px-4 py-2'>{runTitle(run)}</td>
                  <td className='px-4 py-2'>
                    <span className='flex items-center gap-1.5'>
                      {run.kind === 'work' ? (
                        <Wrench className='size-3.5' />
                      ) : (
                        <MessageCircle className='size-3.5' />
                      )}
                      {t(`workspace.kind.${run.kind}`)}
                    </span>
                  </td>
                  <td className='px-4 py-2'>
                    <Badge variant={statusBadgeVariant(run.status as never)}>
                      {t(`work.status.${run.status}`, run.status)}
                    </Badge>
                  </td>
                  <td className='px-4 py-2 font-mono text-xs'>{run.model}</td>
                  <td className='px-4 py-2 text-xs text-muted-foreground'>
                    {run.updated_time
                      ? new Date(run.updated_time * 1000).toLocaleString()
                      : ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('workspace.assetsSection')}</h2>
      {!loading && files.length === 0 ? (
        <p className='text-muted-foreground text-sm'>{t('workspace.emptyAssets')}</p>
      ) : (
        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
          {files.map((file) => (
            <Card key={file.id}>
              <CardHeader>
                <CardTitle className='flex items-center gap-2 text-sm'>
                  {file.kind === 'image' ? (
                    <ImageIcon className='size-4' />
                  ) : (
                    <FileText className='size-4' />
                  )}
                  <span className='max-w-[160px] truncate'>{file.name}</span>
                </CardTitle>
                <CardDescription>
                  {new Date(file.created_time * 1000).toLocaleString()}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <p className='text-muted-foreground text-xs'>
                  {t(`workspace.assetKind.${file.kind}`)} · {formatBytes(file.size)}
                </p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
};

export default Workspace;
