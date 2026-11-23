import { ListError, ListLoading, ListPages, useStudioPage } from '@/components/StudioList';
import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FileText, Image as ImageIcon, MessageCircle, Wrench } from 'lucide-react';

import { formatStudioDate } from '@/helpers/render';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { statusBadgeVariant } from '@/pages/Work/workApi';
import { formatBytes, workspaceApi, type WorkspaceRun } from './workspaceApi';
import KnowledgeLibraries from './KnowledgeLibraries';

const Workspace: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const runList = useStudioPage(workspaceApi.listRuns);
  const fileList = useStudioPage(workspaceApi.listFiles);
  const runs = runList.items;
  const files = fileList.items;

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

      <KnowledgeLibraries />

      <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('workspace.runsSection')}</h2>
      {runList.loading ? (
        <ListLoading />
      ) : runList.failed ? (
        <ListError retry={runList.reload} />
      ) : runs.length === 0 ? (
        <p className='text-muted-foreground mb-8 text-sm'>{t('workspace.emptyRuns')}</p>
      ) : (
        <div className='mb-4 overflow-x-auto rounded-xl border'>
          <table className='w-full min-w-[38rem] text-sm'>
            <thead>
              <tr className='border-b bg-muted/40 text-start text-xs text-muted-foreground'>
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
                      ? formatStudioDate(run.updated_time)
                      : ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {!runList.loading && !runList.failed && <ListPages page={runList.page} more={runList.more} onPage={runList.setPage} />}
      <h2 className='mb-3 mt-6 text-sm font-medium text-muted-foreground'>{t('workspace.assetsSection')}</h2>
      {fileList.loading ? (
        <ListLoading cards />
      ) : fileList.failed ? (
        <ListError retry={fileList.reload} />
      ) : files.length === 0 ? (
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
                  {formatStudioDate(file.created_time)}
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
      {!fileList.loading && !fileList.failed && <ListPages page={fileList.page} more={fileList.more} onPage={fileList.setPage} />}
    </div>
  );
};

export default Workspace;
